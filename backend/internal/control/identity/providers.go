package identity

import (
	"github.com/google/wire"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
)

var ProviderSet = wire.NewSet(postgres.New, wire.Bind(new(Backend), new(*postgres.Store)), wire.Bind(new(domain.GatewayScopes), new(*postgres.Store)), wire.Bind(new(domain.AccessEvaluator), new(*postgres.Store)), wire.Bind(new(domain.ScopeResolver), new(*postgres.Store)), New)
