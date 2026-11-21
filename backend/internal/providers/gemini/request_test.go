package gemini

import (
	"context"
	"io"
	"testing"
)

func TestGenerateWire(t *testing.T) {
	const url = "https://example.invalid/v1beta/models/model:generateContent"
	const raw = `{ "contents": [], "unknown": {"value":9007199254740993} }`
	tests := []struct{ name, apiKey, token, header, value string }{
		{"key", "fixture", "", "x-goog-api-key", "fixture"},
		{"oauth", "", "fixture", "Authorization", "Bearer fixture"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, err := Generate(ctx, url, tt.apiKey, tt.token, []byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			if req.Method != "POST" || req.URL.String() != url || req.Header.Get(tt.header) != tt.value ||
				req.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected request %s %s %v", req.Method, req.URL, req.Header)
			}
			got, err := io.ReadAll(req.Body)
			if err != nil || string(got) != raw {
				t.Fatalf("body = %q, error = %v", got, err)
			}
			cancel()
			if req.Context().Err() != context.Canceled {
				t.Fatal("request did not inherit cancellation")
			}
		})
	}
}

func TestCodeAssistStreamingWire(t *testing.T) {
	action := Action{Model: "model", Name: "streamGenerateContent", Stream: true}
	req, err := action.CodeAssist(context.Background(), "https://example.invalid/", "project", "fixture", []byte(`{"contents":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(req.Body)
	if err != nil || string(got) != `{"model":"model","project":"project","request":{"contents":[]}}` {
		t.Fatalf("body = %q, error = %v", got, err)
	}
	if req.URL.String() != "https://example.invalid/v1internal:streamGenerateContent?alt=sse" ||
		req.Header.Get("Authorization") != "Bearer fixture" || req.UserAgent() == "" {
		t.Fatalf("unexpected Code Assist request %s %v", req.URL, req.Header)
	}
}
