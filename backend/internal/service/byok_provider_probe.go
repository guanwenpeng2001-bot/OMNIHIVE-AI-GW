package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/antigravity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
)

// ProbeBYOKModels checks isolated proposed material against model-specific or
// native authenticated endpoints for providers without an ordinary catalog GET.
func (s *AccountTestService) ProbeBYOKModels(ctx context.Context, a *Account, models []string) ConnectionProbe {
	if a != nil && a.IsVertexServiceAccount() {
		return s.probeBYOKVertex(ctx, a, models)
	}
	result := ConnectionProbe{Network: "unknown", Authorization: "unknown", Discovery: "unsupported", Models: []string{}}
	if s == nil || s.cfg == nil || a == nil || len(models) == 0 || len(models) > 100 {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var verified []string
	switch {
	case a.Platform == PlatformBedrock && a.Type == AccountTypeBedrock:
		for _, model := range models {
			if ctx.Err() != nil {
				break
			}
			req, err := BuildBedrockCatalogRequest(ctx, a, "availability", model, "")
			if err != nil {
				continue
			}
			raw, status := s.byokResponse(req)
			if status != http.StatusOK {
				continue
			}
			availability, err := ParseBedrockModelAvailability(raw)
			if err == nil && availability.ModelID == model && availability.AuthorizationStatus == "AUTHORIZED" && availability.AgreementAvailability.Status == "AVAILABLE" && availability.EntitlementAvailability == "AVAILABLE" && availability.RegionAvailability == "AVAILABLE" {
				verified = append(verified, model)
			}
		}
	case a.Platform == PlatformAntigravity && a.Type == AccountTypeOAuth:
		if len(antigravity.BaseURLs) == 0 {
			return result
		}
		request := func(token string) ([]byte, int) {
			req, err := antigravity.NewAPIRequestWithURL(ctx, antigravity.BaseURLs[0], "fetchAvailableModels", token, []byte(`{}`))
			if err != nil {
				return nil, 0
			}
			if token == "" {
				req.Header.Del("Authorization")
			}
			return s.byokResponse(req)
		}
		raw, status := request(a.GetCredential("access_token"))
		var catalog antigravity.FetchAvailableModelsResponse
		if status != http.StatusOK || json.Unmarshal(raw, &catalog) != nil {
			return result
		}
		_, control := request("")
		if control != http.StatusUnauthorized && control != http.StatusForbidden {
			return result
		}
		for _, model := range models {
			if _, ok := catalog.Models[model]; ok {
				verified = append(verified, model)
			}
		}
	case a.Platform == PlatformCursor && a.Type == AccountTypeOAuth:
		if _, err := s.validateUpstreamBaseURL(cursor.APIBaseURL); err != nil {
			return result
		}
		ids, _, err := s.fetchCursorOAuthCatalog(ctx, a)
		if err != nil {
			return result
		}
		control := &Account{ID: a.ID, WorkspaceID: a.WorkspaceID, Platform: a.Platform, Type: a.Type, Credentials: map[string]any{}}
		_, _, err = s.fetchCursorOAuthCatalog(ctx, control)
		var failure *UpstreamModelSyncError
		if !errors.As(err, &failure) || failure.Kind != UpstreamModelSyncErrorCredentials {
			return result
		}
		for _, model := range models {
			if slices.Contains(ids, model) {
				verified = append(verified, model)
			}
		}
	}
	if len(verified) > 0 {
		return ConnectionProbe{Network: "passed", Authorization: "passed", Discovery: "passed", Models: verified}
	}
	return result
}

func (s *AccountTestService) byokResponse(req *http.Request) ([]byte, int) {
	if _, err := s.validateUpstreamBaseURL(req.URL.Scheme + "://" + req.URL.Host); err != nil {
		return nil, 0
	}
	shared, err := httpclient.GetClient(httpclient.Options{Timeout: 8 * time.Second, ValidateResolvedIP: true})
	if err != nil {
		return nil, 0
	}
	client := *shared
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, 0
	}
	return raw, resp.StatusCode
}
