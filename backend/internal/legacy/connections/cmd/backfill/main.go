package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/connections/migrate"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Connection backfill failed:", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	enc, err := repository.NewAESEncryptor(cfg)
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	for {
		r, err := migrate.Batch(ctx, db, enc, 100)
		if err != nil {
			return err
		}
		fmt.Printf("mapped=%d quarantined=%d\n", r.Mapped, r.Quarantined)
		if r.Mapped+r.Quarantined < 100 {
			return nil
		}
	}
}
