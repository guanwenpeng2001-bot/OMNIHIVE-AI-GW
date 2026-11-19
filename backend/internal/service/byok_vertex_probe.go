package service

import (
	"bytes"
	"context"
	"encoding/json"

	"net/http"
	"regexp"
	"time"
)

var byokVertexID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,127}$`)

// probeBYOKVertex uses the isolated proposed key, including on rotation.
// A shared account token cache could otherwise verify a replacement with an old token.
func (s *AccountTestService) probeBYOKVertex(ctx context.Context, a *Account, models []string) ConnectionProbe {
	result := ConnectionProbe{Network: "unknown", Authorization: "unknown", Discovery: "unsupported", Models: []string{}}
	if s == nil || s.cfg == nil || a == nil || !a.IsVertexServiceAccount() || (a.Platform != PlatformGemini && a.Platform != PlatformAnthropic) || !byokVertexID.MatchString(a.VertexProjectID()) || len(models) == 0 || len(models) > 100 {
		return result
	}
	if _, err := s.validateUpstreamBaseURL(vertexDefaultTokenURL); err != nil {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	token, err := getVertexServiceAccountAccessToken(ctx, nil, a)
	if err != nil {
		return result
	}
	result.Network = "passed"
	for _, model := range models {
		if ctx.Err() != nil {
			break
		}
		if !byokVertexID.MatchString(model) {
			continue
		}
		endpoint, body, field, err := byokVertexCount(a, model)
		if err != nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		raw, status := s.byokResponse(req)
		// Ignore additional provider-specific fields, which can include nested objects.
		var doc map[string]json.RawMessage
		if status != http.StatusOK || json.Unmarshal(raw, &doc) != nil {
			continue
		}
		var count int64
		if json.Unmarshal(doc[field], &count) == nil && count > 0 {
			result.Models = append(result.Models, model)
		}
	}
	if len(result.Models) > 0 {
		result.Authorization = "passed"
		result.Discovery = "passed"
	}
	return result
}

func byokVertexCount(a *Account, model string) (string, []byte, string, error) {
	project, location := a.VertexProjectID(), a.VertexLocation(model)
	if a.Platform == PlatformAnthropic {
		endpoint, err := buildVertexAnthropicURL(project, location, "count-tokens", false)
		body, _ := json.Marshal(map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "test"}}})
		return endpoint, body, "input_tokens", err
	}
	endpoint, err := buildVertexGeminiURL(project, location, model, "countTokens", false)
	return endpoint, []byte(`{"contents":[{"role":"user","parts":[{"text":"test"}]}]}`), "totalTokens", err
}
