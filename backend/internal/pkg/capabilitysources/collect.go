package capabilitysources

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Capture struct {
	Observation Observation `json:"observation"`
	ETag        string      `json:"etag,omitempty"`
	NotModified bool        `json:"not_modified,omitempty"`
}

// Collect makes one bounded request. An ETag is sent only when a prior successful
// response supplied it. Failed or partial responses never replace prior evidence.
// A transport may be injected for fixture tests; redirects cannot leave the exact
// approved endpoint, including when the injected client has a permissive policy.
func Collect(ctx context.Context, client *http.Client, source string, previous *Capture) (Capture, error) {
	endpoint, err := Endpoint(source)
	if err != nil {
		return Capture{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Capture{}, err
	}
	request.Header.Set("Accept", "application/json")
	if previous != nil && previous.Observation.ID == source && previous.ETag != "" {
		request.Header.Set("If-None-Match", previous.ETag)
	}
	bounded := http.Client{}
	if client != nil {
		bounded = *client
	}
	// Public source collection must not reuse account/session cookies.
	bounded.Jar = nil
	bounded.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next.URL.String() != endpoint || len(via) > 2 {
			return fmt.Errorf("redirect outside approved capability endpoint")
		}
		return nil
	}
	response, err := bounded.Do(request)
	if err != nil {
		return Capture{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotModified {
		if previous == nil || previous.Observation.ID != source || previous.ETag == "" || len(previous.Observation.Records) == 0 {
			return Capture{}, fmt.Errorf("304 without prior evidence")
		}
		result := *previous
		result.NotModified = true
		return result, nil // Preserve the original evidence timestamp.
	}
	if response.StatusCode != http.StatusOK {
		return Capture{}, fmt.Errorf("capability source HTTP %d", response.StatusCode)
	}
	if response.ContentLength > MaxBodyBytes {
		return Capture{}, fmt.Errorf("capability response too large")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxBodyBytes+1))
	if err != nil {
		return Capture{}, err
	}
	observation, err := Parse(source, body, time.Now().UTC().Format(time.DateOnly))
	if err != nil {
		return Capture{}, err
	}
	return Capture{Observation: observation, ETag: response.Header.Get("ETag")}, nil
}
