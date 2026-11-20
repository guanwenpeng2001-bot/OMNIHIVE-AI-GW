package handler

import (
	"database/sql"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	requestshttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/requests"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
)

// ProvideRequestsHandler reuses the identity evaluator for project grants.
// The same handler provides request export operations to the identity dispatcher.
func ProvideRequestsHandler(db *sql.DB, access *postgres.Store, cfg *config.Config) *requestshttp.Handler {
	return requestshttp.New(db, access, cfg.JWT.Secret)
}
