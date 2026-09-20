package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Official retirement date: 2026-03-30. Native catalog APIs can retain historical
// entries; do not advertise them as callable or revive their historical prices.
// https://help.aliyun.com/zh/model-studio/rate-limit
func IsRetiredQianwenModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "qwen1.5-")
}

// fetchQianwenModelPages retains the complete upstream entries so subsequent
// metadata extraction sees every page. Pagination never follows upstream URLs.
func (s *AccountTestService) fetchQianwenModelPages(req *http.Request, account *Account) ([]string, []byte, error) {
	return fetchQianwenModelPages(req, resolveModelsListReadLimit(s.cfg), func(req *http.Request) (*http.Response, error) {
		return s.doUpstreamModelsRequest(req, upstreamModelsProxyURL(account), account)
	})
}

func fetchQianwenModelPages(req *http.Request, limit int64, do func(*http.Request) (*http.Response, error)) (models []string, catalogBody []byte, discoveryErr error) {
	catalogDiscovery, _ := req.Context().Value(catalogDiscoveryKey{}).(bool)
	var entries []json.RawMessage
	validatedPages := 0
	if catalogDiscovery {
		entries = []json.RawMessage{}
	}
	// Failed continuations and invalid optional metadata must not turn discovered
	// IDs into an authoritative removal. Preview consumes this typed partial result.
	defer func() {
		if len(entries) == 0 && validatedPages == 0 {
			return
		}
		merged, marshalErr := json.Marshal(struct {
			Data []json.RawMessage `json:"data"`
		}{entries})
		if marshalErr != nil {
			return
		}
		partialModels, metadata, parseErr := extractUpstreamModelCatalog(merged, false, PlatformQianwen)
		if parseErr != nil {
			return
		}
		if discoveryErr == nil {
			discoveryErr = validateUpstreamModelMetadata(metadata)
		}
		if discoveryErr == nil {
			return
		}
		models, catalogBody = partialModels, merged
		discoveryErr = &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorUpstream, Message: "Qianwen model discovery is partial; previous availability retained", Err: discoveryErr,
			PartialCatalog: &UpstreamModelCatalog{Models: []string{}, DiscoveredModels: partialModels, CandidateModels: partialModels, Metadata: metadata, CatalogSource: "partial",
				Warnings: []UpstreamModelSyncWarning{{Code: "upstream_model_discovery_partial", Message: "Qianwen model discovery contains incomplete pages or invalid entries; previous availability must be retained"}}},
		}
	}()
	seen := map[string]bool{}
	seenModels := map[string]bool{}
	nativeTotal, nativeCount := -1, 0
	var total int64
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		resp, err := do(req)
		if err != nil || resp == nil || resp.Body == nil {
			return nil, nil, newUpstreamModelSyncUpstreamError("Failed to request Qianwen model list", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit-total+1))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if catalogDiscovery {
				return nil, nil, newUpstreamModelDiscoveryHTTPError(resp.StatusCode, body)
			}
			return nil, nil, &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorUpstream, Message: fmt.Sprintf("Upstream model list request failed with HTTP %d", resp.StatusCode), StatusCode: resp.StatusCode}
		}
		if readErr != nil {
			return nil, nil, newUpstreamModelSyncUpstreamError("Failed to read Qianwen model list", readErr)
		}
		total += int64(len(body))
		if total > limit {
			return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen model catalog is too large", nil)
		}
		var page struct {
			Data       []json.RawMessage `json:"data"`
			LastID     string            `json:"last_id"`
			HasMore    bool              `json:"has_more"`
			Next       json.RawMessage   `json:"next"`
			NextCursor json.RawMessage   `json:"next_cursor"`
			Success    *bool             `json:"success"`
			Output     *struct {
				Total  *int              `json:"total"`
				Page   int               `json:"page_no"`
				Models []json.RawMessage `json:"models"`
			} `json:"output"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, nil, newUpstreamModelSyncUpstreamError("Invalid Qianwen model list JSON", err)
		}
		if catalogDiscovery && page.Output == nil && page.Data == nil {
			return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen list field missing", nil)
		}
		if nativeTotal >= 0 && page.Output == nil {
			return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen model pagination schema changed", nil)
		}
		if page.Output != nil {
			p := page.Output
			if page.Success == nil || !*page.Success || (catalogDiscovery && p.Models == nil) || p.Total == nil || *p.Total < 0 || p.Page != pageNumber+1 {
				return nil, nil, newUpstreamModelSyncUpstreamError("Invalid Qianwen native model page", nil)
			}
			if nativeTotal == -1 {
				nativeTotal = *p.Total
			}
			if nativeTotal != *p.Total {
				return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen model catalog changed during pagination; retry sync", nil)
			}
			page.Data = p.Models
			nativeCount += len(p.Models)
			if nativeCount > nativeTotal {
				return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen model page exceeds catalog total", nil)
			}
			page.HasMore = nativeCount < nativeTotal
		}
		if page.Success != nil && !*page.Success {
			return nil, nil, newUpstreamModelSyncUpstreamError("Unsuccessful Qianwen model list response", nil)
		}
		for _, raw := range page.Data {
			var entry map[string]json.RawMessage
			if json.Unmarshal(raw, &entry) != nil {
				return nil, nil, newUpstreamModelSyncUpstreamError("Invalid Qianwen model entry", nil)
			}
			var id string
			field := "id"
			if page.Output != nil {
				field = "model"
			}
			if json.Unmarshal(entry[field], &id) != nil || strings.TrimSpace(id) == "" {
				return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen model ID missing", nil)
			}
			id = strings.TrimSpace(id)
			if !catalogDiscovery && IsRetiredQianwenModel(id) {
				continue
			}
			if seenModels[id] {
				continue
			}
			seenModels[id] = true
			entry["id"], _ = json.Marshal(id)
			normalized, _ := json.Marshal(entry)
			entries = append(entries, normalized)
		}
		if page.Data != nil {
			validatedPages++
		}
		if qianfanHasValue(page.Next) || qianfanHasValue(page.NextCursor) {
			return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen continuation does not match the native page or after cursor contract", nil)
		}
		if !page.HasMore {
			merged, err := json.Marshal(struct {
				Data []json.RawMessage `json:"data"`
			}{entries})
			if err != nil {
				return nil, nil, newUpstreamModelSyncUpstreamError("Invalid Qianwen model entries", err)
			}
			models, err := extractUpstreamModelIDs(merged)
			if err != nil || (!catalogDiscovery && len(models) == 0) {
				return nil, nil, newUpstreamModelSyncUpstreamError("Upstream returned no supported models", err)
			}
			return models, merged, nil
		}
		if len(page.Data) == 0 || (page.Output == nil && (strings.TrimSpace(page.LastID) == "" || seen[page.LastID])) {
			return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen model pagination did not advance", nil)
		}
		seen[page.LastID] = true
		next := req.Clone(req.Context())
		query := next.URL.Query()
		if page.Output == nil {
			query.Set("after", page.LastID)
		}
		if query.Has("page_no") || (page.Output != nil) {
			query.Set("page_no", strconv.Itoa(pageNumber+2))
		}
		next.URL.RawQuery = query.Encode()
		req = next
	}
	return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen model pagination exceeded 100 pages", nil)
}
