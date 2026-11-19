package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Independent hosts can reverify the original chain even after an offline rebuild.
const extensionLegacyEvidence = "provenance.s2plugin"

func (i *PluginPackageInstaller) verifyLegacyEvidence(entry *zip.File, target PluginManifest) error {
	o := target.Origin
	if o == nil {
		return nil
	}
	if entry == nil || target.Files[extensionLegacyEvidence] != o.ArchiveSHA256 {
		return errors.New("signed v1 archive evidence missing")
	}
	data, err := readPluginZipFile(entry, i.cfg.Plugins.MaxUploadBytes)
	if err != nil {
		return err
	}
	if pluginSHA(data) != o.ArchiveSHA256 {
		return errors.New("v1 archive evidence digest mismatch")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	// Reject nested v2 envelopes before entering the version-dispatching inspector.
	rawFile, err := z.Open(pluginManifestFilename)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(rawFile, (2<<20)+1))
	_ = rawFile.Close()
	var header struct {
		Version int `json:"schema_version"`
	}
	if err != nil || len(raw) > 2<<20 || json.Unmarshal(raw, &header) != nil || header.Version != 1 || pluginSHA(raw) != o.ManifestSHA256 {
		return errors.New("invalid original v1 manifest evidence")
	}
	legacy, _, status, err := i.inspectArchive(z)
	if err != nil {
		return fmt.Errorf("verify original v1 signature: %w", err)
	}
	if status != PluginSignatureTrusted || legacy.ID != o.PluginID || legacy.Version != target.Version {
		return errors.New("untrusted or mismatched v1 predecessor")
	}
	host, _, _ := legacy.Requires.hostVersions()
	if (legacy.Requires.OmniHive != "" && legacy.Requires.Sub2API != "") || host != target.Requires.OmniHive {
		return errors.New("ambiguous or mismatched v1 host mapping")
	}
	if len(target.Capabilities) != 2 {
		return errors.New("invalid v1 capability mapping")
	}
	ids := map[string]bool{}
	for _, c := range target.Capabilities {
		ids[c.ID] = true
	}
	if !ids["provider"] || !ids["ui"] {
		return errors.New("invalid v1 capability mapping")
	}
	remaining := i.cfg.Plugins.MaxUncompressedBytes
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name, err := normalizePluginArchivePath(f.Name)
		if err != nil {
			return err
		}
		if name == pluginSignatureFilename {
			b, e := readPluginZipFile(f, 64<<10)
			if e != nil {
				return e
			}
			var sig PluginSignature
			if json.Unmarshal(b, &sig) != nil || sig.KeyID != o.KeyID {
				return errors.New("original v1 signer mismatch")
			}
			continue
		}
		if name == pluginManifestFilename {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(r, remaining+1))
		_ = r.Close()
		if err != nil || n > remaining || hex.EncodeToString(h.Sum(nil)) != legacy.Files[name] {
			return errors.New("original v1 payload evidence mismatch")
		}
		remaining -= n
	}
	return nil
}
