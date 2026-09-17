package modelcapabilities

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	modelpricing "github.com/Wei-Shaw/sub2api/resources/model-pricing"
)

//go:embed release-manifest.json
var releaseManifest string

// The trust root is separately reviewed and compiled; no capture command writes it.
//
//go:embed release-pin.json
var releasePin string

type ReleaseIdentity struct {
	ApprovedGeneration int64  `json:"approved_generation"`
	ManifestSHA256     string `json:"manifest_sha256"`
}

func hash(body []byte) string { h := sha256.Sum256(body); return hex.EncodeToString(h[:]) }

// VerifyRelease checks externally supplied bytes against a trusted pin and the
// previously approved identity. Callers must obtain trustedPin independently of
// a candidate; using a candidate's own pin would grant it approval authority.
func VerifyRelease(body, manifestBody, trustedPin []byte, previous *ReleaseIdentity) (ReleaseIdentity, error) {
	var manifest struct {
		SchemaVersion      int               `json:"schema_version"`
		ApprovedGeneration int64             `json:"approved_generation"`
		CatalogSHA256      string            `json:"catalog_sha256"`
		BaseDataSHA256LF   string            `json:"base_data_sha256_lf"`
		RecordCount        int               `json:"record_count"`
		AdapterVersion     string            `json:"adapter_version"`
		AliasVersion       string            `json:"alias_version"`
		RulesVersion       string            `json:"rules_version"`
		CodeVersions       map[string]string `json:"code_versions"`
	}
	var pin ReleaseIdentity
	fail := func() (ReleaseIdentity, error) { return ReleaseIdentity{}, fmt.Errorf("untrusted capability release") }
	if json.Unmarshal(manifestBody, &manifest) != nil || json.Unmarshal(trustedPin, &pin) != nil {
		return fail()
	}
	if manifest.SchemaVersion != 1 || manifest.ApprovedGeneration <= 0 || pin.ApprovedGeneration != manifest.ApprovedGeneration || pin.ManifestSHA256 != hash(manifestBody) || manifest.CatalogSHA256 != hash(body) {
		return fail()
	}
	if manifest.BaseDataSHA256LF != hash(bytes.ReplaceAll(modelpricing.JSON(), []byte("\r\n"), []byte("\n"))) {
		return fail()
	}
	if manifest.AdapterVersion == "" || manifest.AliasVersion == "" || manifest.RulesVersion == "" || len(manifest.CodeVersions) == 0 {
		return fail()
	}
	if previous != nil && (pin.ApprovedGeneration < previous.ApprovedGeneration || (pin.ApprovedGeneration == previous.ApprovedGeneration && pin.ManifestSHA256 != previous.ManifestSHA256)) {
		return fail()
	}
	var catalog struct {
		SchemaVersion int               `json:"schema_version"`
		GeneratedAt   time.Time         `json:"generated_at"`
		Records       []json.RawMessage `json:"records"`
	}
	if json.Unmarshal(body, &catalog) != nil || catalog.SchemaVersion != 1 || catalog.GeneratedAt.Unix() != pin.ApprovedGeneration || len(catalog.Records) == 0 || len(catalog.Records) != manifest.RecordCount {
		return fail()
	}
	return pin, nil
}

func Release() ReleaseIdentity {
	// Also verifies catalog bytes; returning copies cannot mutate the trust root.
	JSON()
	var identity ReleaseIdentity
	_ = json.Unmarshal([]byte(releasePin), &identity)
	return identity
}

func ManifestJSON() []byte { return []byte(strings.ReplaceAll(releaseManifest, "\r\n", "\n")) }
