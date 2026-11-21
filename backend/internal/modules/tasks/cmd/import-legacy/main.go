// Command import-legacy copies Redis image metadata and outstanding batch leases.
// It performs read-only Redis operations and can resume a SCAN cursor after interruption.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

func run(ctx context.Context) error {
	source := flag.String("source", "images", "images or batch-leases")
	start := flag.Uint64("cursor", 0, "Redis SCAN cursor to resume")
	prefix := flag.String("lock-prefix", "", "deployed batch queue lock prefix")
	flag.Parse()
	if *source != "images" && *source != "batch-leases" {
		return fmt.Errorf("invalid source")
	}
	dsn := os.Getenv("TASK_IMPORT_POSTGRES_DSN")
	redisURL := os.Getenv("TASK_IMPORT_REDIS_URL")
	if dsn == "" || redisURL == "" {
		return fmt.Errorf("TASK_IMPORT_POSTGRES_DSN and TASK_IMPORT_REDIS_URL are required")
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return fmt.Errorf("invalid Redis configuration")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("invalid PostgreSQL configuration")
	}
	defer db.Close()
	rd := redis.NewClient(opt)
	defer func() { _ = rd.Close() }()
	m := tasks.LegacyImport{DB: db, Redis: rd, BatchLockPrefix: *prefix}
	scan := m.Page
	if *source == "batch-leases" {
		scan = m.BatchLeases
	}
	cursor := *start
	for {
		next, err := scan(ctx, cursor)
		if err != nil {
			return fmt.Errorf("import failed at cursor %d: %w", cursor, err)
		}
		fmt.Printf("source=%s next_cursor=%d\n", *source, next)
		if next == 0 {
			return nil
		}
		cursor = next
	}
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := run(ctx); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
