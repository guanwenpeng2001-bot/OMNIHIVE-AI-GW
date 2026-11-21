package service

import (
	_ "embed"
	"encoding/json"
	"reflect"
)

// Exact pre-Cursor factory seed from migration 197; migrations stay immutable.
//
//go:embed channel_telemetry_factory_platforms.json
var preCursorFactoryPlatformSeed []byte

// SeedCursorDimensionIntoFactoryConfig adds the new default dimension only to an
// untouched factory configuration. Saved custom configurations, including explicit
// removal, retain their meaning. The next normal config save persists this default.
func SeedCursorDimensionIntoFactoryConfig(cfg *ChannelTelemetryConfig) {
	if cfg == nil || cfg.UpdatedBy != nil {
		return
	}
	var factory []ChannelTelemetryPlatformConfig
	if json.Unmarshal(preCursorFactoryPlatformSeed, &factory) != nil {
		return
	}
	if !reflect.DeepEqual(cfg.Platforms, factory) {
		return
	}
	cfg.Platforms = append(cfg.Platforms, ChannelTelemetryPlatformConfig{Platform: PlatformCursor, Enabled: true, Models: DefaultCursorModelIDs()})
}
