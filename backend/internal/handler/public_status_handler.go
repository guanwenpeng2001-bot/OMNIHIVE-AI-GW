package handler

import (
	"database/sql"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	statushttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/status"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func ProvideStatusHandler(db *sql.DB, access *postgres.Store, cfg *config.Config, ops *service.OpsService) *statushttp.Handler {
	return statushttp.New(db, access, cfg.JWT.Secret, ops.IsMonitoringEnabled)
}
