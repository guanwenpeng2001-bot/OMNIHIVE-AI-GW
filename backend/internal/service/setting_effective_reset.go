package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

// SettingOverrideResetter is implemented by the production repository with a
// single SQL transaction.
type SettingOverrideResetter interface {
	DeleteMultiple(ctx context.Context, keys []string) error
}

type SettingResetHooks struct {
	Refresh    func(context.Context) error
	ImageReset func(context.Context) error
}

type SettingResetAuditor func(context.Context, []string)

var ErrSettingResetUnavailable = infraerrors.ServiceUnavailable("CONFIG_RESET_UNAVAILABLE", "transactional settings reset is not configured")

func (s *SettingService) SetConfigResetAuditor(auditor SettingResetAuditor) {
	s.resetAuditor = auditor
}

func (s *SettingService) ResetConfigOverrides(ctx context.Context, keys []string, hooks SettingResetHooks) ([]string, error) {
	registry := make(map[string]SettingRegistration)
	for _, registration := range s.SettingRegistry() {
		registry[registration.Key] = registration
	}
	normalized := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	groups := make(map[string]struct{})
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		registration, ok := registry[key]
		if !ok {
			return nil, infraerrors.BadRequest("CONFIG_KEY_UNKNOWN", "configuration key is not registered").WithMetadata(map[string]string{"key": key})
		}
		if registration.Authority != SettingAuthorityDatabaseOverride || !registration.Resettable {
			return nil, infraerrors.BadRequest("CONFIG_KEY_NOT_RESETTABLE", "configuration key is not a resettable database override").WithMetadata(map[string]string{"key": key})
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
		if registration.ResetGroup != "" {
			groups[registration.ResetGroup] = struct{}{}
		}
	}
	if len(normalized) == 0 {
		return nil, infraerrors.BadRequest("CONFIG_KEYS_REQUIRED", "keys must contain at least one resettable configuration key")
	}
	resetter, ok := s.settingRepo.(SettingOverrideResetter)
	if !ok {
		return nil, ErrSettingResetUnavailable
	}
	if _, requested := groups["image_storage"]; requested && hooks.ImageReset == nil {
		return nil, ErrSettingResetUnavailable
	}
	if _, requested := groups["ops"]; requested && hooks.Refresh == nil {
		return nil, ErrSettingResetUnavailable
	}
	for _, registration := range s.SettingRegistry() {
		if _, ok := groups[registration.ResetGroup]; ok && registration.ResetGroup != "" && registration.Resettable {
			if _, ok := seen[registration.Key]; !ok {
				seen[registration.Key] = struct{}{}
				normalized = append(normalized, registration.Key)
			}
		}
	}

	deleteKeys := make([]string, 0, len(normalized))
	for _, key := range normalized {
		if strings.Contains(key, ".") && key != "image_storage_config" {
			continue
		}
		deleteKeys = append(deleteKeys, key)
	}
	if len(deleteKeys) > 0 {
		if err := resetter.DeleteMultiple(ctx, deleteKeys); err != nil {
			return nil, fmt.Errorf("delete configuration overrides: %w", err)
		}
	}
	if _, ok := groups["image_storage"]; ok {
		if err := hooks.ImageReset(ctx); err != nil {
			return nil, err
		}
	}
	if _, ok := groups["ops"]; ok {
		if err := hooks.Refresh(ctx); err != nil {
			return nil, fmt.Errorf("reload configuration after reset: %w", err)
		}
	}
	if s.onUpdate != nil {
		s.onUpdate()
	}
	if current, err := s.GetAllSettings(ctx); err == nil {
		s.refreshCachedSettings(current)
	}
	slog.Info("configuration overrides reset", "audit", true, "keys", normalized)
	if auditor := s.resetAuditor; auditor != nil {
		auditor(ctx, normalized)
	}
	sort.Strings(normalized)
	return normalized, nil
}
