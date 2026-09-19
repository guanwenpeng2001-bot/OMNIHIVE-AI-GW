package capabilitysources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (transport fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestSourceFixtures(t *testing.T) {
	for _, source := range []string{"models.dev", "openrouter", "litellm"} {
		t.Run(source, func(t *testing.T) {
			body, err := os.ReadFile("testdata/" + source + ".json")
			if err != nil {
				t.Fatal(err)
			}
			result, err := Parse(source, body, "2026-09-19")
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Records) != 1 || result.Records[0]["input_modalities"] == nil {
				t.Fatal("lost explicit empty array")
			}
			invalid := strings.ReplaceAll(string(body), `[]`, `"invalid"`)
			if _, err := Parse(source, []byte(invalid), "2026-09-19"); err == nil {
				t.Fatal("invalid array accepted")
			}
		})
	}
	for name, body := range map[string]string{
		"partial":   `{"data":[{"id":"model"}],"has_more":true}`,
		"cursor":    `{"data":[{"id":"model"}],"next_cursor":"next"}`,
		"type":      `{"data":[{"id":"model","context_length":"123"}]}`,
		"null":      `{"data":[{"id":"model","supported_parameters":null}]}`,
		"duplicate": `{"data":[{"id":"model"},{"id":"model"}]}`,
		"trailing":  `{"data":[{"id":"model"}]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse("openrouter", []byte(body), "2026-09-19"); err == nil {
				t.Fatal("invalid document accepted")
			}
		})
	}
	missing, err := Parse("openrouter", []byte(`{"data":[{"id":"model"}]}`), "2026-09-19")
	if err != nil || len(missing.Records[0]) != 3 {
		t.Fatal("missing fields inferred", err)
	}
	if _, err := Parse("other", []byte(`{}`), "2026-09-19"); err == nil {
		t.Fatal("unknown source")
	}
	if _, err := Parse("openrouter", make([]byte, MaxBodyBytes+1), "2026-09-19"); err == nil {
		t.Fatal("oversize body")
	}
	for _, source := range []string{"models.dev", "litellm"} {
		body, err := os.ReadFile("testdata/" + source + ".json")
		if err != nil {
			t.Fatal(err)
		}
		for _, replacement := range []string{`"false"`, `null`, `1`} {
			invalid := strings.ReplaceAll(string(body), "false", replacement)
			if _, err := Parse(source, []byte(invalid), "2026-09-19"); err == nil {
				t.Fatalf("%s accepted nonboolean %s", source, replacement)
			}
		}
	}
	for source, body := range map[string]string{
		"models.dev": `{"native":{"models":{"model":{}}}}`,
		"litellm":    `{"model":{"litellm_provider":"native"}}`,
	} {
		observation, err := Parse(source, []byte(body), "2026-09-19")
		if err != nil || len(observation.Records[0]) != 3 {
			t.Fatalf("%s inferred missing fields: %v", source, err)
		}
	}
}

func TestCollectorBoundaries(t *testing.T) {
	for _, scenario := range []string{"success", "etag", "unconditional", "redirect", "offline", "timeout", "oversize", "partial", "unsolicited304"} {
		t.Run(scenario, func(t *testing.T) {
			previous := &Capture{Observation: Observation{ID: "openrouter", VerifiedAt: "2026-09-01", Records: []map[string]any{{"model_id": "old"}}}, ETag: `"prior"`}
			if scenario == "unconditional" {
				previous.ETag = ""
			}
			if scenario == "unsolicited304" {
				previous = nil
			}
			calls := 0
			client := &http.Client{Transport: fixtureTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					t.Fatal("redirect escaped whitelist")
				}
				if scenario == "offline" {
					return nil, errors.New("offline fixture")
				}
				if scenario == "timeout" {
					return nil, context.DeadlineExceeded
				}
				if scenario == "unconditional" && request.Header.Get("If-None-Match") != "" {
					t.Fatal("invented conditional header")
				}
				if scenario == "etag" && request.Header.Get("If-None-Match") != `"prior"` {
					t.Fatal("lost conditional header")
				}
				response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"fresh"}]}`)), Request: request}
				switch scenario {
				case "redirect":
					response.StatusCode = 302
					response.Header.Set("Location", "https://example.invalid/secret")
				case "etag", "unsolicited304":
					response.StatusCode = 304
				case "oversize":
					response.ContentLength = MaxBodyBytes + 1
				case "partial":
					response.Body = io.NopCloser(strings.NewReader(`{"data":[{"id":"fresh"}],"has_more":true}`))
				}
				return response, nil
			})}
			result, err := Collect(context.Background(), client, "openrouter", previous)
			valid := scenario == "success" || scenario == "etag" || scenario == "unconditional"
			if valid != (err == nil) {
				t.Fatalf("unexpected result: %v", err)
			}
			if scenario == "etag" && (!result.NotModified || result.Observation.VerifiedAt != "2026-09-01") {
				t.Fatal("304 changed evidence")
			}
			if previous != nil && previous.Observation.Records[0]["model_id"] != "old" {
				t.Fatal("modified previous capture")
			}
		})
	}
}
