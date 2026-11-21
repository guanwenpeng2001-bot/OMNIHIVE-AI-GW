package repository

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/restoregate"
)

// The snapshot's separate pg_dump session needs ACCESS SHARE. Upgrade only
// after it closes, before any archive DDL can request locks in dump order.
const lockRestoreTables = `SET LOCAL lock_timeout='5s';

 CREATE FUNCTION pg_temp.lock_restore_tables(exclusive boolean) RETURNS text LANGUAGE plpgsql AS $lock$
 DECLARE rel record; mode text := CASE WHEN exclusive THEN 'ACCESS EXCLUSIVE' ELSE 'SHARE' END;
 BEGIN
 FOR rel IN SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE c.relkind IN ('r','p') AND n.nspname !~ '^pg_' AND n.nspname<>'information_schema' ORDER BY c.oid
 LOOP
 BEGIN
 EXECUTE format('LOCK TABLE ONLY %I.%I IN %s MODE',rel.nspname,rel.relname,mode);
 EXCEPTION WHEN deadlock_detected OR lock_not_available THEN
 RETURN format('%s %I.%I',CASE WHEN SQLSTATE='40P01' THEN 'deadlock' ELSE 'lock_timeout' END,rel.nspname,rel.relname);
 END;
 END LOOP;
 RETURN '';
 END $lock$;
 SELECT pg_temp.lock_restore_tables(false);`

// pg_dump only cleans constraints known at backup time. Keep the definitions
// for relations which survive the archive, including target-only tables.
const detachForeignKeys = `SET LOCAL search_path = pg_catalog;
CREATE TEMP TABLE restore_saved_fks ON COMMIT DROP AS
 SELECT conrelid, conname, pg_get_constraintdef(c.oid) AS definition,
 obj_description(c.oid, 'pg_constraint') AS comment
 FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid
 WHERE contype='f' AND conparentid=0 AND r.relpersistence <> 't';
DO $fk$ DECLARE fk record; BEGIN
 FOR fk IN SELECT * FROM pg_temp.restore_saved_fks ORDER BY conrelid, conname LOOP
 EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I',fk.conrelid::regclass,fk.conname);
 END LOOP;
END $fk$;
RESET search_path;`

const reattachForeignKeys = `SET LOCAL search_path = pg_catalog;
DO $fk$ DECLARE fk record; BEGIN
 FOR fk IN SELECT f.* FROM pg_temp.restore_saved_fks f JOIN pg_class r ON r.oid=f.conrelid
 ORDER BY f.conrelid, f.conname LOOP
 EXECUTE format('ALTER TABLE %s ADD CONSTRAINT %I %s',fk.conrelid::regclass,fk.conname,fk.definition);
 IF fk.comment IS NOT NULL THEN
 EXECUTE format('COMMENT ON CONSTRAINT %I ON %s IS %L',fk.conname,fk.conrelid::regclass,fk.comment);
 END IF;
 END LOOP;
END $fk$;`

var psqlState = regexp.MustCompile(`^psql:.*:[0-9]+: (?:ERROR|FATAL):  ([0-9A-Z]{5}):`)

// Keep only a bounded line prefix and a SQLSTATE. DETAIL, CONTEXT, SQL,
// paths and the server's free-form error message never enter the journal.
type restoreStderr struct {
	prefix []byte
	state  string
}

func (s *restoreStderr) Write(p []byte) (int, error) {
	size := len(p)
	for len(p) > 0 {
		line, rest, complete := bytes.Cut(p, []byte{'\n'})
		s.prefix = append(s.prefix, line[:min(len(line), 512-len(s.prefix))]...)
		if complete {
			if match := psqlState.FindSubmatch(s.prefix); match != nil {
				s.state = string(match[1])
			}
			s.prefix = s.prefix[:0]
		}
		p = rest
	}
	return size, nil
}

// RestoreChecked keeps the table locks, replacement and content checks in one
// backend transaction. An EOF, cancellation or failed check before the explicit
// COMMIT rolls it back. No password, COPY value or SQL is returned in errors.
func (d *PgDumper) RestoreChecked(ctx context.Context, source, checks string, locked, applied, verified func() error) (result error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := d.commandContext(ctx, "psql", "-X", "-qAt", "-w", "-h", d.cfg.Host, "-p", strconv.Itoa(d.cfg.Port), "-U", d.cfg.User, "-d", d.cfg.DBName,
		"-v", "VERBOSITY=verbose", "-v", "ON_ERROR_STOP=on", "-v", "archive="+source, "-v", "checks="+checks, "-f", "-")
	d.setCommandEnv(cmd)
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	var diagnostic restoreStderr
	cmd.Stderr = &diagnostic
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	defer func() {
		cancel()
		input.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
		if result != nil && diagnostic.state != "" {
			result = errors.Join(result, restoregate.PostgresError(diagnostic.state))
		}
	}()
	reader := bufio.NewReader(output)
	marker := uuid.NewString()
	step := func(commands string, locking bool, callback func() error) error {
		if _, err := fmt.Fprintf(input, "%s\n\\echo %s\n", commands, marker); err != nil {
			return err
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("psql stopped before checkpoint: %w", err)
			}
			if locking && (strings.HasPrefix(line, "deadlock ") || strings.HasPrefix(line, "lock_timeout ")) {
				return restoregate.LockError(strings.TrimSpace(line))
			}
			if line == marker+"\n" {
				return callback()
			}
		}
	}
	if err = step("BEGIN;\n"+lockRestoreTables, true, locked); err != nil {
		return err
	}
	if err = step("SELECT pg_temp.lock_restore_tables(true);", true, func() error { return nil }); err != nil {
		return err
	}
	if err = step(detachForeignKeys+"\n"+`\i :archive`+"\n"+reattachForeignKeys, false, applied); err != nil {
		return err
	}
	if err = step(`\i :checks`, false, verified); err != nil {
		return err
	}
	if _, err = io.WriteString(input, "COMMIT;\n"); err != nil {
		return err
	}
	if err = input.Close(); err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, reader)
	return errors.Join(err, cmd.Wait())
}
