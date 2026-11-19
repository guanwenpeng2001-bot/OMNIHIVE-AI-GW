// Command omnihive-retire produces read-only deployment retirement reports.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/retirement"
	"github.com/lib/pq"
)

const usage = "Usage: omnihive-retire (scan|report|plan) [--format=json|markdown] [--dry-run]"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	if len(args) == 0 || (args[0] != "scan" && args[0] != "report" && args[0] != "plan") {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", "json", "")
	dryRun := flags.Bool("dry-run", true, "")
	err := flags.Parse(args[1:])
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	if err != nil || flags.NArg() != 0 || !*dryRun || (*format != "json" && *format != "markdown") {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	dsn := os.Getenv("OMNIHIVE_REFACTOR_DSN")
	if dsn == "" {
		fmt.Fprintln(stderr, "OMNIHIVE_REFACTOR_DSN is required")
		return 2
	}
	app, err := openDB(dsn)
	if err != nil {
		fmt.Fprintln(stderr, "Invalid application database configuration")
		return 1
	}
	defer app.Close()
	var restore *sql.DB
	if dsn := os.Getenv("OMNIHIVE_RESTORE_JOURNAL_DSN"); dsn != "" {
		restore, err = openDB(dsn)
		if err != nil {
			fmt.Fprintln(stderr, "Invalid restore journal configuration")
			return 1
		}
		defer restore.Close()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	report, err := retirement.Scan(ctx, app, restore)
	if err != nil {
		fmt.Fprintln(stderr, "Could not read retirement inventory")
		return 1
	}
	var output interface{ Markdown() string } = report
	if args[0] == "plan" {
		output = report.Plan()
	}
	if *format == "markdown" {
		_, err = io.WriteString(stdout, output.Markdown())
	} else {
		err = json.NewEncoder(stdout).Encode(output)
	}
	if err != nil {
		fmt.Fprintln(stderr, "Could not write retirement inventory")
		return 1
	}
	return 0
}

func openDB(dsn string) (db *sql.DB, err error) {
	// lib/pq panics for unsupported libpq environment variables.
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
