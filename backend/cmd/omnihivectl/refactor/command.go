// Package refactor implements the backend migration commands for omnihivectl.
package refactor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/migration"
	"github.com/lib/pq"
)

const usage = "Usage: omnihivectl refactor (inventory|status) --format=json"

// Run accepts arguments after the refactor subcommand. It returns an exit
// code so the parent CLI can own process and signal handling.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	if len(args) == 0 || (args[0] != "inventory" && args[0] != "status") {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", "json", "")
	parseErr := flags.Parse(args[1:])
	if errors.Is(parseErr, flag.ErrHelp) {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	if parseErr != nil || flags.NArg() != 0 || *format != "json" {
		fmt.Fprintln(stderr, usage)
		return 2
	}

	appDSN := os.Getenv("OMNIHIVE_REFACTOR_DSN")
	if appDSN == "" {
		fmt.Fprintln(stderr, "OMNIHIVE_REFACTOR_DSN is required")
		return 2
	}
	app, err := openDB(appDSN)
	if err != nil {
		fmt.Fprintln(stderr, "Could not configure application database connection")
		return 1
	}
	defer app.Close()

	var restore *sql.DB
	if dsn := os.Getenv("OMNIHIVE_RESTORE_JOURNAL_DSN"); dsn != "" {
		restore, err = openDB(dsn)
		if err != nil {
			fmt.Fprintln(stderr, "Could not configure restore journal database connection")
			return 1
		}
		defer restore.Close()
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	reader := migration.NewReader(app, restore)
	read := reader.Inventory
	if args[0] == "status" {
		read = reader.Status
	}
	report, err := read(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "Could not read migration report from application and restore sources")
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		fmt.Fprintln(stderr, "Could not write migration report")
		return 1
	}
	return 0
}

func openDB(dsn string) (db *sql.DB, err error) {
	// lib/pq panics on unsupported libpq environment settings when it parses
	// a DSN. Convert driver configuration failures to the same redacted error.
	defer func() {
		if recover() != nil {
			err = errors.New("unsupported PostgreSQL connection settings")
		}
	}()
	connector, err := pq.NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}
