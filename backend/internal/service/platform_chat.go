package service

import "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"

// IsChatOnlyPlatform limits runtime support independently of model metadata.
func IsChatOnlyPlatform(platform string) bool {
	descriptor, _ := domain.LookupPlatform(platform)
	return descriptor.ChatOnly
}
