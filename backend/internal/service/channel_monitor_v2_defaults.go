package service

import (
	_ "embed"
	"encoding/json"
	"reflect"
)

// Exact pre-Cursor factory seed from migration 197; migrations stay immutable.
//go:embed channel_monitor_v2_factory_platforms.json
var channelMonitorV2FactoryPlatforms []byte

// SeedChannelMonitorV2Cursor adds the new default dimension only to an untouched
// factory configuration. Saved custom configurations, including explicit removal,
// retain their meaning. The next normal config save persists this default.
func SeedChannelMonitorV2Cursor(cfg *ChannelMonitorV2Config) {
	if cfg == nil || cfg.UpdatedBy != nil {
		return
	}
	var factory []ChannelMonitorV2PlatformConfig
	if json.Unmarshal(channelMonitorV2FactoryPlatforms, &factory) != nil {
		return
	}
	if !reflect.DeepEqual(cfg.Platforms, factory) {
		return
	}
	cfg.Platforms = append(cfg.Platforms, ChannelMonitorV2PlatformConfig{Platform: PlatformCursor, Enabled: true, Models: DefaultCursorModelIDs()})
}
