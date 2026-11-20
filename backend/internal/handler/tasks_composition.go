package handler

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

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
	reader.video = taskVideoResults{native: images}
	store := tasks.New(db, gateway, encryptor, reader, reader)
	store.Video = reader.video
	return taskhttp.New(store, access, cfg)
}

func ProvideImageTaskCenter(db *sql.DB, cfg *config.Config, access identity.AccessEvaluator, scopes identity.GatewayScopes,
	catalogReader catalog.CatalogReader, keys *service.APIKeyService, subscriptions *service.SubscriptionService,
	images *OpenAIGatewayHandler, settings *service.ImageStorageSettingService, factory service.BackupObjectStoreFactory,
	batches *service.BatchImageDownloadService, commands *service.BatchImagePublicService, encryptor service.SecretEncryptor) *taskhttp.Handler {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, enabled, err := settings.PrivateTaskArtifactStore(ctx, factory)
	if err != nil {
		slog.Warn("Image task artifact storage unavailable")
		store, enabled = nil, false
	}
	if !cfg.Totp.EncryptionKeyConfigured {
		encryptor = nil
	}
	return NewImageTaskCenter(db, cfg, access, scopes, catalogReader, keys, subscriptions, images,
		&taskArtifactObjects{store: store, enabled: enabled}, batches, commands, encryptor)
}
