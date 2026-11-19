package postgres

import (
	"context"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/verification"
)

func (s *Store) Compatibility(ctx context.Context, entries []string) (map[string][]catalog.Compatibility, error) {
	return verification.ReadPublished(ctx, s.db, entries)
}
