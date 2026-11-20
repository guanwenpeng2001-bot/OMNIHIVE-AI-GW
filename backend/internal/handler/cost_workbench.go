package handler

import (
	"database/sql"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/billing"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func ProvideCostWorkbench(db *sql.DB, access *postgres.Store, totp *service.TotpService, cfg *config.Config) *billing.Handler {
	return billing.New(db, access, totp, cfg.JWT.Secret)
}
