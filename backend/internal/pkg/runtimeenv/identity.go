package runtimeenv

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	DataDirKey       = "OMNIHIVE_DATA_DIR"
	ContainerDataDir = "/app/data"
	InstallLockFile  = ".installed"
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

func HasInstallLock() bool {
	dirs := []string{ContainerDataDir, "."}
	if dir := DataDir(); dir != "" {
		dirs = []string{dir}
	}
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, InstallLockFile)); !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

func SkipSetupEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SKIP_SETUP"))) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}
