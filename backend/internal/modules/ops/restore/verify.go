package restore

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Accept pg_dump's schema-qualified identifiers, including quoted column names.
const identifier = `(?:"(?:[^"\r\n]|"")+"|[a-z_][a-z0-9_$]*)`

var copyHeader = regexp.MustCompile(`^COPY (` + identifier + `\.` + identifier + `) \((` + identifier + `(?:, ` + identifier + `)*)\) FROM stdin;\n$`)

// Replay each COPY into a typed temporary table. PostgreSQL parses legacy zone
// offsets and emits both sides in the same session. EXCEPT ALL retains duplicates.
func writeVerification(sql io.Reader, out io.Writer) error {
	w := bufio.NewWriter(out)
	fmt.Fprintln(w, "CREATE TEMP TABLE omnihive_verified (rel oid PRIMARY KEY, matches boolean NOT NULL CHECK (matches)) ON COMMIT DROP;")
	r := bufio.NewReaderSize(sql, 64*1024)
	var table, columns string
	for {
		line, err := r.ReadSlice('\n')
		if err == io.EOF && len(line) == 0 {
			break
		}
		if table != "" && err == nil && bytes.Equal(line, []byte("\\.\n")) {
			_, _ = w.Write(line)
			projection := "SELECT ROW(" + columns + `)::text COLLATE "C" FROM `
			actual, expected := projection+"ONLY "+table, projection+"pg_temp.omnihive_expected"
			fmt.Fprintf(w, "INSERT INTO pg_temp.omnihive_verified SELECT '%s'::regclass, NOT EXISTS ((%s EXCEPT ALL %s) UNION ALL (%s EXCEPT ALL %s));\n", strings.ReplaceAll(table, "'", "''"), actual, expected, expected, actual)
			fmt.Fprintln(w, "DROP TABLE pg_temp.omnihive_expected;")
			table = ""
			continue
		}
		if table == "" && bytes.HasPrefix(line, []byte("COPY ")) {
			match := copyHeader.FindSubmatch(line)
			if match == nil {
				return invalidArchive
			}
			table, columns = string(match[1]), string(match[2])
			fmt.Fprintf(w, "CREATE TEMP TABLE omnihive_expected AS SELECT %s FROM ONLY %s WITH NO DATA;\nCOPY pg_temp.omnihive_expected FROM stdin;\n", columns, table)
			continue
		}
		for {
			if table != "" {
				if _, e := w.Write(line); e != nil {
					return e
				}
			}
			if !errors.Is(err, bufio.ErrBufferFull) {
				break
			}
			line, err = r.ReadSlice('\n')
		}
		if err != nil {
			return err
		}
	}
	if table != "" {
		return invalidArchive
	}
	fmt.Fprintln(w, `DO $check$ BEGIN
 IF EXISTS (SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE c.relkind='r' AND n.nspname !~ '^pg_' AND n.nspname<>'information_schema'
 AND NOT EXISTS (SELECT FROM pg_depend d WHERE d.classid='pg_class'::regclass AND d.objid=c.oid AND d.deptype='e')
 AND NOT EXISTS (SELECT FROM pg_temp.omnihive_verified v WHERE v.rel=c.oid))
 THEN RAISE EXCEPTION 'unexpected table after restore'; END IF; END $check$;`)
	return w.Flush()
}
