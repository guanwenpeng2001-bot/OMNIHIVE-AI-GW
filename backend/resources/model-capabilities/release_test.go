package modelcapabilities_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	capabilities "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-capabilities"
)

func TestRealReleaseOfflineAndImmutable(t *testing.T) {
	t.Chdir(t.TempDir())
	body := capabilities.JSON()
	var c struct {
		Records []service.CapabilityRecord `json:"records"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Records) != 912 {
		t.Fatalf("actual released records: %d", len(c.Records))
	}
	var manifest struct {
		RecordCount int `json:"record_count"`
	}
	if err := json.Unmarshal(capabilities.ManifestJSON(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.RecordCount != 912 || manifest.RecordCount != len(c.Records) {
		t.Fatalf("manifest/catalog count mismatch: manifest=%d catalog=%d", manifest.RecordCount, len(c.Records))
	}
	for _, record := range c.Records {
		if record.ModelID == "" || record.Profile == "" {
			t.Fatal("identity lost")
		}
	}
	pin, _ := json.Marshal(capabilities.Release())
	if _, err := capabilities.VerifyRelease(body, capabilities.ManifestJSON(), pin, nil); err != nil {
		t.Fatal(err)
	}
	body[0] = '!'
	if bytes.Equal(body, capabilities.JSON()) {
		t.Fatal("caller mutated embed")
	}
	if _, err := capabilities.VerifyRelease(body, capabilities.ManifestJSON(), pin, nil); err == nil {
		t.Fatal("tampered body accepted")
	}
}

func TestReleaseRejectsSameGenerationDifferentHashAndRollback(t *testing.T) {
	prior := capabilities.Release()
	raw := capabilities.ManifestJSON()
	var manifest map[string]any
	_ = json.Unmarshal(raw, &manifest)
	manifest["alias_version"] = "unreviewed-aliases"
	changed, _ := json.Marshal(manifest)
	h := sha256.Sum256(changed)
	candidate := capabilities.Release()
	candidate.ManifestSHA256 = hex.EncodeToString(h[:])
	pin, _ := json.Marshal(candidate)
	if _, err := capabilities.VerifyRelease(capabilities.JSON(), changed, pin, &prior); err == nil {
		t.Fatal("same generation hash change accepted")
	}
	pin, _ = json.Marshal(prior)
	prior.ApprovedGeneration++
	if _, err := capabilities.VerifyRelease(capabilities.JSON(), raw, pin, &prior); err == nil {
		t.Fatal("rollback accepted")
	}
}

func TestGoPublicControlRoundTrip(t *testing.T) {
	cases := []string{
		`{"reasoning_control":"unknown"}`,
		`{"input_modalities":[],"output_modalities":[],"supported_methods":[],"supports_reasoning":false}`,
		`{"supports_reasoning":false,"reasoning_control":{"kind":"none"}}`,
		`{"supports_reasoning":true,"reasoning_control":{"kind":"effort","effort_values":["low","high"],"default":"low"}}`,
		`{"reasoning_control":{"kind":"budget","budget_min":0,"budget_max":1024}}`,
		`{"reasoning_control":{"kind":"toggle","toggle_values":[false,true]}}`,
		`{"reasoning_control":{"kind":"adaptive","adaptive":false,"supported_kinds":["adaptive","budget"]}}`,
		`{"reasoning_control":{"kind":"fixed"}}`,
		`{"reasoning_control":{"kind":"effort","effort_values":[]}}`,
		`{"reasoning_control":{"kind":"toggle","toggle_values":[],"supported_kinds":[]}}`,
	}
	for _, input := range cases {
		var before service.CapabilityRecord
		if err := json.Unmarshal([]byte(input), &before); err != nil {
			t.Fatal(err)
		}
		internal, err := json.Marshal(before)
		if err != nil {
			t.Fatal(err)
		}
		public := internal
		var after service.CapabilityRecord
		if err := json.Unmarshal(public, &after); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("round trip lost control: %s -> %s", input, public)
		}
		var fields map[string]any
		_ = json.Unmarshal(public, &fields)
		if _, leaked := fields["control"]; leaked {
			t.Fatal("internal control leaked")
		}
	}
}

func TestGoPublishedCatalogRoundTrip(t *testing.T) {
	var c struct {
		SchemaVersion int                        `json:"schema_version"`
		GeneratedAt   string                     `json:"generated_at"`
		Description   string                     `json:"description"`
		Records       []service.CapabilityRecord `json:"records"`
	}
	if err := json.Unmarshal(capabilities.JSON(), &c); err != nil {
		t.Fatal(err)
	}
	internal, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var after struct {
		SchemaVersion int                        `json:"schema_version"`
		GeneratedAt   string                     `json:"generated_at"`
		Description   string                     `json:"description"`
		Records       []service.CapabilityRecord `json:"records"`
	}
	if err := json.Unmarshal(internal, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, after) {
		t.Fatal("published schema lost information in Go round trip")
	}
}
