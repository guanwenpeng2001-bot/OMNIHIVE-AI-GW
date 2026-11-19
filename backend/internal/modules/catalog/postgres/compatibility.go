package postgres

import (
	"context"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/verification"
)

func (s *Store) Compatibility(ctx context.Context, entries []string, now time.Time) (map[string][]catalog.Compatibility, error) {
	return verification.ReadPublished(ctx, s.db, entries, now)
}
