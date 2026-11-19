package handler

import (
	"database/sql"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	taskhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// NewImageTaskCenter assembles the v2 transport independently of native route
// registration. Pass a stable private object store for the image artifact prefix.
func NewImageTaskCenter(db *sql.DB, cfg *config.Config, access identity.AccessEvaluator, scopes identity.GatewayScopes,
	catalogReader catalog.CatalogReader, keys *service.APIKeyService, subscriptions *service.SubscriptionService,
	images *OpenAIGatewayHandler, objects service.BackupObjectStore, batches *service.BatchImageDownloadService,
	commands *service.BatchImagePublicService, encryptor service.SecretEncryptor) *taskhttp.Handler {
	reader := NewImageTaskArtifacts(objects, batches, commands)
	gateway := &ImageTaskGateway{DB: db, Access: access, Scopes: scopes, Catalog: catalogReader, Keys: keys, Subscriptions: subscriptions, Images: images, Objects: objects}
	if configured, ok := encryptor.(interface{ EncryptionKeyConfigured() bool }); ok && !configured.EncryptionKeyConfigured() {
		encryptor = nil
	}
	return taskhttp.New(tasks.New(db, gateway, encryptor, reader, reader), access, cfg)
}
