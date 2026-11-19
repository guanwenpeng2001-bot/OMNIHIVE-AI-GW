package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"

	v2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2"
)

func pluginSHA(data []byte) string { d := sha256.Sum256(data); return hex.EncodeToString(d[:]) }

// WriteExtension signs the exact emitted manifest and validates every payload
// against it. Publishers run this offline, using the host's public trust policy.
func (i *PluginPackageInstaller) WriteExtension(ctx context.Context, w io.Writer, m PluginManifest, fs *os.Root, privateKey ed25519.PrivateKey) error {
	if m.SchemaVersion != 2 {
		return errors.New("v2 manifest required")
	}
	if err := m.Validate(); err != nil {
		return err
	}
	public, err := i.extensionPublisher(m)
	if err != nil {
		return err
	}
	if len(privateKey) != ed25519.PrivateKeySize || !bytes.Equal(public, privateKey.Public().(ed25519.PublicKey)) {
		return errors.New("private key does not match the authorized v2 publisher")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	signature, err := json.Marshal(PluginSignature{Algorithm: "ed25519", KeyID: m.PublisherID, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, raw))})
	if err != nil {
		return err
	}
	z := zip.NewWriter(w)
	write := func(name string, data []byte) error {
		file, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		return err
	}
	if err = write(pluginManifestFilename, raw); err != nil {
		return err
	}
	if err = write(pluginSignatureFilename, signature); err != nil {
		return err
	}
	paths := make([]string, 0, len(m.Files))
	for path := range m.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) > pluginArchiveMaxFiles-2 {
		return errors.New("too many package entries")
	}
	remaining := i.cfg.Plugins.MaxUncompressedBytes - int64(len(raw)+len(signature))
	for _, path := range paths {
		if err = ctx.Err(); err != nil {
			return err
		}
		f, err := fs.Open(path)
		if err != nil {
			return err
		}
		limit := remaining
		if path == m.Runtimes[v2.RuntimeKey].Path {
			limit = min(limit, int64(v2.MaxModule))
		}
		b, err := io.ReadAll(io.LimitReader(f, limit+1))
		_ = f.Close()
		if err != nil || int64(len(b)) > limit || pluginSHA(b) != m.Files[path] {
			return fmt.Errorf("signed payload size or hash mismatch: %s", path)
		}
		if err = write(path, b); err != nil {
			return err
		}
		remaining -= int64(len(b))
	}
	return z.Close()
}

// RebuildV1 verifies the original package, including payloads for other
// platforms, before constructing a new signed envelope from the reviewed WASI build.
func (i *PluginPackageInstaller) RebuildV1(ctx context.Context, source, module io.Reader, w io.Writer, target, publisher string, privateKey ed25519.PrivateKey) error {
	src, err := i.Install(ctx, source, nil)
	if err != nil {
		return err
	}
	defer os.RemoveAll(src.InstallPath)
	defer os.Remove(src.ArtifactPath)
	if src.Manifest.SchemaVersion != 1 || src.SignatureStatus != PluginSignatureTrusted {
		return errors.New("migration requires a trusted signed v1 archive")
	}
	old := src.Manifest
	r := old.Requires
	if r.Sub2API != "" && r.OmniHive != "" {
		return errors.New("ambiguous v1 host identity requires manual review")
	}
	if !src.Compatibility.Compatible {
		return errors.New("v1 host range requires manual review")
	}
	ar, err := zip.NewReader(bytes.NewReader(src.ArtifactData), int64(len(src.ArtifactData)))
	if err != nil {
		return err
	}
	_, raw, _, err := i.inspectArchive(ar)
	if err != nil {
		return err
	}
	var sig PluginSignature
	for _, entry := range ar.File {
		if entry.Name == pluginSignatureFilename {
			b, e := readPluginZipFile(entry, 64<<10)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(b, &sig); e != nil {
				return e
			}
			break
		}
	}
	m := old
	m.SchemaVersion = 2
	m.ProductID = v2.ProductID
	m.PublisherID = publisher
	m.ID = target
	m.Origin = &v2.Origin{PluginID: old.ID, KeyID: sig.KeyID, ArchiveSHA256: pluginSHA(src.ArtifactData), ManifestSHA256: pluginSHA(raw)}
	required, _, _ := r.hostVersions()
	m.Requires = PluginRequirements{OmniHive: required, PluginProtocol: 2, TransportAPI: 1, UIBridge: 1}
	m.Capabilities = []PluginCapability{{ID: v2.Provider, Platform: PlatformOpenAI, AccountType: AccountTypeOAuth}, {ID: v2.UI}}
	// The signed rebuild keeps UI assets and replaces every native executable.
	const path = "runtime.wasm"
	m.Runtimes = map[string]PluginRuntime{v2.RuntimeKey: {Path: path}}
	m.Files = map[string]string{}
	for p, h := range old.Files {
		if len(p) > 3 && p[:3] == "ui/" {
			m.Files[p] = h
		}
	}
	mod, err := io.ReadAll(io.LimitReader(module, v2.MaxModule+1))
	if err != nil {
		return err
	}
	if len(mod) > v2.MaxModule || len(mod) < 8 || !slices.Equal(mod[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) {
		return errors.New("rebuilt WASI module required")
	}
	m.Files[path] = pluginSHA(mod)
	root, err := os.OpenRoot(src.InstallPath)
	if err != nil {
		return err
	}
	defer root.Close()
	m.Files[extensionLegacyEvidence] = m.Origin.ArchiveSHA256
	if err = root.WriteFile(extensionLegacyEvidence, src.ArtifactData, 0600); err != nil {
		return err
	}
	f, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(mod)
	closeErr := f.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	return i.WriteExtension(ctx, w, m, root, privateKey)
}
