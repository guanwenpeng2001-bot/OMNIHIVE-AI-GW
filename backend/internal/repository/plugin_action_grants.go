package repository

import (
	"context"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/extensions/actions"
)

func (r *pluginRepository) LookupExtensionAction(ctx context.Context, target actions.Target) (actions.Grant, error) {
	return actions.Lookup(ctx, r.db, target)
}
