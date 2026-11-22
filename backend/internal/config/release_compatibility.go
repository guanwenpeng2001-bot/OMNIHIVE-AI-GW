package config

import (
	_ "embed"
	"encoding/json"
	"strings"

	"golang.org/x/mod/semver"
)

//go:embed release_compatibility.json
var releaseCompatibilityJSON []byte

type ReleaseCompatibility struct {
	SchemaRollbackFloor   string `json:"schema_rollback_floor"`
	SemanticRollbackFloor string `json:"semantic_rollback_floor"`
}

// CurrentReleaseCompatibility and release packaging consume the same embedded
// machine-readable source. A malformed build input must fail before serving.
func CurrentReleaseCompatibility() ReleaseCompatibility {
	var value ReleaseCompatibility
	if err := json.Unmarshal(releaseCompatibilityJSON, &value); err != nil {
		panic("invalid release compatibility build input")
	}
	for _, raw := range []string{value.SchemaRollbackFloor, value.SemanticRollbackFloor} {
		version := "v" + strings.TrimPrefix(raw, "v")
		if !semver.IsValid(version) || semver.Prerelease(version) != "" || semver.Build(version) != "" {
			panic("invalid release compatibility floor")
		}
	}
	return value
}

func MinimumRollbackVersion() string {
	value := CurrentReleaseCompatibility()
	schema := "v" + strings.TrimPrefix(value.SchemaRollbackFloor, "v")
	semantic := "v" + strings.TrimPrefix(value.SemanticRollbackFloor, "v")
	if semver.Compare(schema, semantic) > 0 {
		return schema
	}
	return semantic
}
