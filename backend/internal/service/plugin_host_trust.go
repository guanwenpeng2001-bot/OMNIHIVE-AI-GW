package service

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	v2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2"
)

var extensionDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ExtensionPublisher struct {
	PublicKey string `json:"public_key"`
	// Each grant belongs to a specific publisher key and plugin ID.
	Grants map[string][]string `json:"grants"`
	// The target plugin ID maps to the explicitly reviewed v1 identity and signer.
	Predecessors map[string]struct {
		PluginID string `json:"plugin_id"`
		KeyID    string `json:"key_id"`
	} `json:"predecessors,omitempty"`
}

type ExtensionTrust struct {
	Publishers map[string]ExtensionPublisher `json:"publishers"`
}

func extensionDenied(id, capability, reason string) error {
	slog.Warn("plugin_capability_denied", "plugin_id", id, "capability", capability, "reason", reason)
	return fmt.Errorf("plugin %s denied %q: %s", id, capability, reason)
}

func validateExtensionManifest(m PluginManifest) error {
	if m.ProductID != v2.ProductID || m.Requires.PluginProtocol != v2.ProtocolVersion || m.Requires.TransportAPI != 1 || m.Requires.UIBridge != 1 {
		return errors.New("unsupported plugin product_id or protocol version")
	}
	if !pluginIDPattern.MatchString(m.ID) || len(m.ID) > 160 || strings.TrimSpace(m.Name) == "" || len(m.Name) > 160 || normalizeSemver(m.Version) == "" || m.PublisherID == "" || len(m.PublisherID) > 160 {
		return errors.New("invalid plugin identity, publisher or version")
	}
	r := m.Requires
	if r.OmniHive == "" || r.Sub2API != "" || r.RecommendedSub2APIVersion != "" || len(r.TestedSub2APIVersions) != 0 {
		return errors.New("v2 requires an unambiguous omnihive host range")
	}
	seen := map[string]bool{}
	if len(m.Capabilities) == 0 {
		return errors.New("missing plugin capabilities")
	}
	for _, cap := range m.Capabilities {
		switch cap.ID {
		case v2.Provider:
			if cap.Platform != PlatformOpenAI || cap.AccountType != AccountTypeOAuth {
				return extensionDenied(m.ID, cap.ID, "unsupported provider scope")
			}
		case v2.UI, v2.Task:
			if cap.Platform != "" || cap.AccountType != "" {
				return extensionDenied(m.ID, cap.ID, "unexpected account scope")
			}
		default:
			return extensionDenied(m.ID, cap.ID, "unsupported capability")
		}
		if seen[cap.ID] {
			return errors.New("duplicate capability")
		}
		seen[cap.ID] = true
	}
	if len(m.Runtimes) != 1 || m.Files[m.Runtimes[v2.RuntimeKey].Path] == "" || !strings.HasSuffix(m.Runtimes[v2.RuntimeKey].Path, ".wasm") {
		return errors.New("v2 requires a signed WASI module")
	}
	if seen[v2.UI] && (!strings.HasPrefix(m.UI.Entrypoint, "ui/") || m.Files[m.UI.Entrypoint] == "") || !seen[v2.UI] && m.UI.Entrypoint != "" {
		return errors.New("UI entrypoint must match the UI capability")
	}
	for path, hash := range m.Files {
		canonical, err := normalizePluginArchivePath(path)
		if err != nil || canonical != path || strings.Contains(path, ":") || !extensionDigest.MatchString(hash) || path == pluginManifestFilename || path == pluginSignatureFilename {
			return fmt.Errorf("invalid signed file %q", path)
		}
	}
	if o := m.Origin; o != nil && (!pluginIDPattern.MatchString(o.PluginID) || o.KeyID == "" || !extensionDigest.MatchString(o.ArchiveSHA256) || !extensionDigest.MatchString(o.ManifestSHA256)) {
		return errors.New("invalid v1 provenance")
	}
	return nil
}

func (i *PluginPackageInstaller) extensionPublisher(m PluginManifest) (ed25519.PublicKey, error) {
	f, err := os.Open(filepath.Join(i.rootDir, "trust-v2.json"))
	if err != nil {
		return nil, extensionDenied(m.ID, "trust", "no readable local trust-v2.json")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("invalid v2 trust file size")
	}
	var policy ExtensionTrust
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&policy) != nil || dec.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("invalid v2 trust configuration")
	}
	root, ok := policy.Publishers[m.PublisherID]
	grants, owned := root.Grants[m.ID]
	if !ok || !owned {
		return nil, extensionDenied(m.ID, "trust", "publisher has no grant for this plugin ID")
	}
	for _, cap := range m.Capabilities {
		if !slices.Contains(grants, cap.ID) {
			return nil, extensionDenied(m.ID, cap.ID, "publisher grant missing")
		}
	}
	if o := m.Origin; o != nil {
		prior, ok := root.Predecessors[m.ID]
		if !ok || prior.PluginID != o.PluginID || prior.KeyID != o.KeyID {
			return nil, extensionDenied(m.ID, "migration", "predecessor ownership not approved")
		}
	}
	key, err := base64.StdEncoding.DecodeString(root.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("invalid v2 publisher public key")
	}
	return key, nil
}

func (i *PluginPackageInstaller) verifyExtensionSignature(file *zip.File, raw []byte, m PluginManifest) (ed25519.PublicKey, error) {
	if file == nil {
		return nil, errors.New("v2 signature.json required")
	}
	key, err := i.extensionPublisher(m)
	if err != nil {
		return nil, err
	}
	data, err := readPluginZipFile(file, 64<<10)
	if err != nil {
		return nil, err
	}
	var sig PluginSignature
	if json.Unmarshal(data, &sig) != nil || sig.KeyID != m.PublisherID || sig.Algorithm != "ed25519" {
		return nil, errors.New("v2 signature identity mismatch")
	}
	signed, err := base64.StdEncoding.DecodeString(sig.Signature)
	if err != nil || !ed25519.Verify(key, raw, signed) {
		return nil, errors.New("invalid v2 manifest signature")
	}
	return key, nil
}
