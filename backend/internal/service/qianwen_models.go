package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// fetchQianwenModelPages retains the complete upstream entries so subsequent
// metadata extraction sees every page. Pagination never follows upstream URLs.
func (s *AccountTestService) fetchQianwenModelPages(req *http.Request, account *Account) ([]string, []byte, error) {
	return fetchQianwenModelPages(req, resolveModelsListReadLimit(s.cfg), func(req *http.Request) (*http.Response, error) {
		return s.doUpstreamModelsRequest(req, upstreamModelsProxyURL(account), account)
	})
}

func fetchQianwenModelPages(req *http.Request, limit int64, do func(*http.Request) (*http.Response, error)) ([]string, []byte, error) {
	var entries []json.RawMessage
	seen := map[string]bool{}
	var total int64
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		resp, err := do(req)
		if err != nil {
			return nil, nil, newUpstreamModelSyncUpstreamError("Failed to request Qianwen model list", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit-total+1))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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
			Data    []json.RawMessage `json:"data"`
			LastID  string            `json:"last_id"`
			HasMore bool              `json:"has_more"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, nil, newUpstreamModelSyncUpstreamError("Invalid Qianwen model list JSON", err)
		}
		entries = append(entries, page.Data...)
		if !page.HasMore {
			merged, err := json.Marshal(struct {
				Data []json.RawMessage `json:"data"`
			}{entries})
			if err != nil {
				return nil, nil, newUpstreamModelSyncUpstreamError("Invalid Qianwen model entries", err)
			}
			models, err := extractUpstreamModelIDs(merged)
			if err != nil || len(models) == 0 {
				return nil, nil, newUpstreamModelSyncUpstreamError("Upstream returned no supported models", err)
			}
			return models, merged, nil
		}
		if len(page.Data) == 0 || strings.TrimSpace(page.LastID) == "" || seen[page.LastID] {
			return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen model pagination did not advance", nil)
		}
		seen[page.LastID] = true
		next := req.Clone(req.Context())
		query := next.URL.Query()
		query.Set("after", page.LastID)
		next.URL.RawQuery = query.Encode()
		req = next
	}
	return nil, nil, newUpstreamModelSyncUpstreamError("Qianwen model pagination exceeded 100 pages", nil)
}
