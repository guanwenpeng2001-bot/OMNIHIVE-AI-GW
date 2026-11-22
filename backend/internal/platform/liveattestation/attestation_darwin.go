//go:build darwin

package liveattestation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

const (
	chatGPTApplicationPath = "/Applications/ChatGPT.app"
	attestationTimeout     = 5 * time.Second
)

type darwinProvider struct {
	appSessionID string
	appPaths     []string
}

type deviceSignals struct {
	SchemaVersion      int      `json:"schemaVersion"`
	PreferredLanguages []string `json:"preferredLanguages"`
	Locale             string   `json:"locale"`
	Timezone           string   `json:"timezone"`
	ScreenSizeSum      int      `json:"screenSizeSum"`
	ScreenScale        float64  `json:"screenScale"`
	AppSessionID       string   `json:"appSessionId"`
}

type macOSSignals struct {
	Locale    string   `json:"locale"`
	Languages []string `json:"languages"`
	Timezone  string   `json:"timezone"`
	Width     float64  `json:"width"`
	Height    float64  `json:"height"`
	Scale     float64  `json:"scale"`
}

func NewProvider() Provider {
	paths := []string{chatGPTApplicationPath}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		paths = append(paths, filepath.Join(home, "Applications", "ChatGPT.app"))
	}
	return &darwinProvider{
		appSessionID: uuid.NewString(),
		appPaths:     paths,
	}
}

func (p *darwinProvider) Check(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, attestationTimeout)
	defer cancel()
	_, _, _, err := p.resolveRuntime(checkCtx)
	return err
}

func (p *darwinProvider) resolveRuntime(ctx context.Context) (string, string, string, error) {
	if runtime.GOARCH != "arm64" {
		return "", "", "", errors.New("live attestation currently requires Apple Silicon; Intel macOS is not supported")
	}
	appPath, err := p.findApplication()
	if err != nil {
		return "", "", "", err
	}
	resourcesPath := filepath.Join(appPath, "Contents", "Resources")
	nodePath := filepath.Join(resourcesPath, "cua_node", "bin", "node")
	modulePath := filepath.Join(resourcesPath, "native", "devicecheck.node")
	for filePath, label := range map[string]string{
		nodePath:   "bundled Node.js runtime",
		modulePath: "DeviceCheck native module",
	} {
		if info, statErr := os.Stat(filePath); statErr != nil || !info.Mode().IsRegular() {
			return "", "", "", fmt.Errorf("%w: ChatGPT app is missing its %s", ErrChatGPTAppMissing, label)
		}
		if err := verifyRuntimePath(filePath); err != nil {
			return "", "", "", err
		}
	}
	// The bundle seal authenticates resources in addition to the main executable.
	const requirement = `anchor apple generic and identifier "com.openai.chat" and certificate leaf[subject.OU] = "2DC432GLL2"`
	if err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", "-R="+requirement, appPath).Run(); err != nil {
		return "", "", "", fmt.Errorf("verify ChatGPT application signature: %w", err)
	}
	for _, path := range []string{nodePath, modulePath} {
		if err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", path).Run(); err != nil {
			return "", "", "", fmt.Errorf("verify ChatGPT runtime signature: %w", err)
		}
	}
	return nodePath, modulePath, "com.openai.chat", nil
}

func (p *darwinProvider) Generate(ctx context.Context) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, attestationTimeout)
	defer cancel()
	nodePath, modulePath, bundleID, err := p.resolveRuntime(runCtx)
	if err != nil {
		return "", err
	}
	signals, err := p.readSignals(runCtx)
	if err != nil {
		return "", err
	}
	signalsJSON, err := json.Marshal(signals)
	if err != nil {
		return "", fmt.Errorf("encode Live attestation signals: %w", err)
	}

	command := exec.CommandContext(runCtx, nodePath, "-e", deviceCheckScript)
	command.Env = []string{
		"PATH=/usr/bin:/bin",
		"OMNIHIVE_DEVICECHECK_MODULE=" + modulePath,
		"OMNIHIVE_ATTESTATION_BUNDLE_ID=" + bundleID,
		"OMNIHIVE_ATTESTATION_SIGNALS=" + string(signalsJSON),
	}
	stdout := attestationOutput{limit: 16 * 1024}
	stderr := attestationOutput{limit: 240}
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return "", errors.New("ChatGPT DeviceCheck token generation timed out")
		}
		reason := strings.TrimSpace(stderr.String())

		if reason == "" {
			reason = err.Error()
		}
		return "", fmt.Errorf("ChatGPT DeviceCheck token generation failed: %s", reason)
	}
	header := strings.TrimSpace(stdout.String())
	if stdout.overflow || len(header) < 20 || !json.Valid([]byte(header)) {
		return "", errors.New("ChatGPT DeviceCheck returned a malformed attestation")
	}
	return header, nil
}

func (p *darwinProvider) findApplication() (string, error) {
	for _, appPath := range p.appPaths {
		info, err := os.Stat(appPath)
		if err == nil && info.IsDir() {
			return appPath, nil
		}
	}
	return "", ErrChatGPTAppMissing
}

// Check each ancestor to prevent an untrusted local user replacing a checked
// binary or redirecting a module path outside the signed bundle.
func verifyRuntimePath(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		// Group 80 is the macOS administrators group (e.g. /Applications).
		if info.Mode()&os.ModeSymlink != 0 || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid())) ||
			info.Mode().Perm()&0002 != 0 || (info.Mode().Perm()&0020 != 0 && stat.Gid != 80) {
			return errors.New("untrusted ChatGPT runtime path")
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func (p *darwinProvider) readSignals(ctx context.Context) (deviceSignals, error) {
	const script = `ObjC.import("Foundation"); ObjC.import("AppKit");
const screen = $.NSScreen.mainScreen;
const frame = screen.frame;
JSON.stringify({
  locale: ObjC.unwrap($.NSLocale.currentLocale.localeIdentifier),
  languages: ObjC.deepUnwrap($.NSLocale.preferredLanguages),
  timezone: ObjC.unwrap($.NSTimeZone.localTimeZone.name),
  width: Number(frame.size.width),
  height: Number(frame.size.height),
  scale: Number(screen.backingScaleFactor)
})`
	output, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script).Output()
	if err != nil {
		return deviceSignals{}, fmt.Errorf("read macOS signals for Live attestation: %w", err)
	}
	var values macOSSignals
	if err := json.Unmarshal(output, &values); err != nil {
		return deviceSignals{}, fmt.Errorf("decode macOS signals for Live attestation: %w", err)
	}
	locale := truncateSignal(values.Locale, 64, "unknown")
	languages := values.Languages
	if len(languages) == 0 {
		languages = []string{locale}
	}
	if len(languages) > 16 {
		languages = languages[:16]
	}
	for index := range languages {
		languages[index] = truncateSignal(languages[index], 64, locale)
	}
	scale := values.Scale
	if scale <= 0 {
		scale = 1
	}
	return deviceSignals{
		SchemaVersion:      1,
		PreferredLanguages: languages,
		Locale:             locale,
		Timezone:           truncateSignal(values.Timezone, 64, "unknown"),
		ScreenSizeSum:      max(0, int(values.Width+values.Height+0.5)),
		ScreenScale:        scale,
		AppSessionID:       truncateSignal(p.appSessionID, 128, uuid.NewString()),
	}, nil
}

func truncateSignal(value string, limit int, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

const deviceCheckScript = `
const addon = require(process.env.OMNIHIVE_DEVICECHECK_MODULE);
const signals = JSON.parse(process.env.OMNIHIVE_ATTESTATION_SIGNALS);
const bundleID = process.env.OMNIHIVE_ATTESTATION_BUNDLE_ID;

function head(major, value) {
  if (value < 24) return Buffer.from([major + value]);
  if (value <= 255) return Buffer.from([major + 24, value]);
  if (value <= 65535) {
    const out = Buffer.allocUnsafe(3);
    out[0] = major + 25;
    out.writeUInt16BE(value, 1);
    return out;
  }
  const out = Buffer.allocUnsafe(5);
  out[0] = major + 26;
  out.writeUInt32BE(value, 1);
  return out;
}
function uint(value) { return head(0, value); }
function text(value) {
  const body = Buffer.from(value, "utf8");
  return Buffer.concat([head(96, body.length), body]);
}
function float(value) {
  if (Number.isSafeInteger(value) && value >= 0) return uint(value);
  const out = Buffer.allocUnsafe(9);
  out[0] = 251;
  out.writeDoubleBE(value, 1);
  return out;
}
function array(values) { return Buffer.concat([head(128, values.length), ...values]); }
function map(entries) {
  return Buffer.concat([head(160, entries.length), ...entries.flatMap(([key, value]) => [uint(key), value])]);
}
function field(key, value) { return Buffer.concat([text(key), text(value)]); }
function base64url(value) {
  return value.toString("base64").replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/u, "");
}

(async () => {
  const result = await addon.generateToken();
  if (!result || !result.supported) throw new Error("DeviceCheck is not supported on this Mac");
  if (!result.tokenBase64) throw new Error("DeviceCheck returned no token");
  const fingerprint = map([
    [0, uint(signals.schemaVersion)],
    [1, array(signals.preferredLanguages.map(text))],
    [2, text(signals.locale)],
    [3, text(signals.timezone)],
    [4, uint(signals.screenSizeSum)],
    [5, float(signals.screenScale)],
    [6, text(signals.appSessionId)]
  ]);
  const fields = [
    field("token", result.tokenBase64),
    field("bundle_id", bundleID),
    Buffer.concat([text("f"), head(64, fingerprint.length), fingerprint])
  ];
  if (result.latencyMs != null) {
    fields.push(Buffer.concat([text("t"), float(result.latencyMs)]));
  }
  const token = "v1." + base64url(Buffer.concat([Buffer.from([160 + fields.length]), ...fields]));
  process.stdout.write(JSON.stringify({v: 1, s: 0, t: token}));
})().catch((error) => {
  process.stderr.write(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});`
