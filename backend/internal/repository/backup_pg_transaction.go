package repository

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/google/uuid"
)

// Take a SHARE lock on existing tables before the snapshot. Ordinary readers
// (including pg_dump) can proceed. Writers and competing DDL must wait.
const lockRestoreTables = `DO $lock$ DECLARE rel record; BEGIN
 FOR rel IN SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE c.relkind IN ('r','p') AND n.nspname !~ '^pg_' AND n.nspname<>'information_schema' ORDER BY c.oid
 LOOP EXECUTE format('LOCK TABLE ONLY %I.%I IN SHARE MODE',rel.nspname,rel.relname); END LOOP;
 END $lock$;`

// RestoreChecked keeps the table locks, replacement and content checks in one
// backend transaction. An EOF, cancellation or failed check before the explicit
// COMMIT rolls it back. No password, COPY value or SQL is returned in errors.
func (d *PgDumper) RestoreChecked(ctx context.Context, source, checks string, locked, applied, verified func() error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := d.commandContext(ctx, "psql", "-X", "-qAt", "--no-password", "-h", d.cfg.Host, "-p", strconv.Itoa(d.cfg.Port), "-U", d.cfg.User, "-d", d.cfg.DBName,
		"-v", "ON_ERROR_STOP=on", "-v", "archive="+source, "-v", "checks="+checks, "-f", "-")
	d.setCommandEnv(cmd)
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
			cmd.Wait()
		}
	}()
	reader := bufio.NewReader(output)
	marker := uuid.NewString()
	step := func(commands string, callback func() error) error {
		if _, err := fmt.Fprintf(input, "%s\n\\echo %s\n", commands, marker); err != nil {
			return err
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("psql stopped before checkpoint: %w", err)
			}
			if line == marker+"\n" {
				return callback()
			}
		}
	}
	if err = step("BEGIN;\n"+lockRestoreTables, locked); err != nil {
		return err
	}
	if err = step(`\i :archive`, applied); err != nil {
		return err
	}
	if err = step(`\i :checks`, verified); err != nil {
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
