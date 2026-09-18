package service

import (
	"regexp"
	"strings"
)

var capabilityReasonerSuffix = regexp.MustCompile(`(?:^|-)(?:reasoner|reasoning|thinking)$`)
var capabilityContextSuffix = regexp.MustCompile(`-(256k|1m)$`)
var capabilityEmbeddingToken = regexp.MustCompile(`-embedding(?:-|$)`)

func inferModelCapabilityHints(modelID string) UpstreamModelMetadata {
	m := UpstreamModelMetadata{ID: modelID}
	if len(modelID) > 512 {
		return m
	}
	name := strings.ToLower(modelID)
	if capabilityEmbeddingToken.MatchString(name) {
		m.TaskType = "embedding"
		m.InputModalities = []string{"text"}
		m.OutputModalities = []string{"text"}
	}
	if !strings.Contains(name, "non-reasoning") && !strings.Contains(name, "non-thinking") && capabilityReasonerSuffix.MatchString(name) {
		yes := true
		m.Reasoning = &yes
	}
	if match := capabilityContextSuffix.FindStringSubmatch(name); len(match) > 0 {
		m.ContextHint = match[1]
	}
	if strings.HasSuffix(name, "-flash") {
		m.FamilyHint = "flash"
	}
	stampCapabilityEvidence(&m, "heuristic", "name-rules-v1", "", modelID, "bounded_suffix", "estimated")
	return m
}
