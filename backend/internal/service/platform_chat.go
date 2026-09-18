package service

import "github.com/Wei-Shaw/sub2api/internal/domain"

// IsChatOnlyPlatform limits runtime support independently of model metadata.
func IsChatOnlyPlatform(platform string) bool {
	descriptor, _ := domain.LookupPlatform(platform)
	return descriptor.ChatOnly
}
