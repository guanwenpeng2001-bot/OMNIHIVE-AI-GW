package runtimeenv

import (
	"log/slog"
	"os"
	"strings"
	"sync"
)

const (
	DataDirKey       = "OMNIHIVE_DATA_DIR"
	ContainerDataDir = "/app/data"
	SystemConfigDir  = "/etc/omnihive"
	LegacyConfigDir  = "/etc/sub2api"
)

var warned sync.Map

func Lookup(current, legacy string) (string, bool) {
	if value, ok := os.LookupEnv(current); ok {
		return value, true
	}
	value, ok := os.LookupEnv(legacy)
	if ok {
		WarnLegacy(legacy, current)
	}
	return value, ok
}

func Get(current, legacy string) string {
	value, _ := Lookup(current, legacy)
	return value
}

func DataDir() string {
	return strings.TrimSpace(Get(DataDirKey, "DATA_DIR"))
}

func WarnLegacy(legacy, current string) {
	if _, seen := warned.LoadOrStore(legacy, struct{}{}); !seen {
		slog.Warn("deprecated OmniHive identifier in use", "legacy", legacy, "replacement", current)
	}
}
