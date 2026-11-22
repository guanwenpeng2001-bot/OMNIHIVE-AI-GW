package logredact

import (
	"strings"
	"testing"
)

func TestRedactText_JSONLike(t *testing.T) {
	in := `{"access_token":"ya29.a0AfH6SMDUMMY","refresh_token":"1//0gDUMMY","other":"ok"}`
	out := RedactText(in)
	if out == in {
		t.Fatalf("expected redaction, got unchanged")
	}
	if want := `"access_token":"***"`; !strings.Contains(out, want) {
		t.Fatalf("expected %q in %q", want, out)
	}
	if want := `"refresh_token":"***"`; !strings.Contains(out, want) {
		t.Fatalf("expected %q in %q", want, out)
	}
}

func TestRedactText_QueryLike(t *testing.T) {
	in := "access_token=ya29.a0AfH6SMDUMMY refresh_token=1//0gDUMMY"
	out := RedactText(in)
	if strings.Contains(out, "ya29") || strings.Contains(out, "1//0") {
		t.Fatalf("expected tokens redacted, got %q", out)
	}
}

func TestRedactText_GOCSPX(t *testing.T) {
	in := "client_secret=GOCSPX-your-client-secret"
	out := RedactText(in)
	if strings.Contains(out, "your-client-secret") {
		t.Fatalf("expected secret redacted, got %q", out)
	}
	if !strings.Contains(out, "client_secret=***") {
		t.Fatalf("expected key redacted, got %q", out)
	}
}

func TestRedactText_ExtraKeyCacheUsesNormalizedSortedKey(t *testing.T) {
	clearExtraTextPatternCache()

	out1 := RedactText("custom_secret=abc", "Custom_Secret", " custom_secret ")
	out2 := RedactText("custom_secret=xyz", "custom_secret")
	if !strings.Contains(out1, "custom_secret=***") {
		t.Fatalf("expected custom key redacted in first call, got %q", out1)
	}
	if !strings.Contains(out2, "custom_secret=***") {
		t.Fatalf("expected custom key redacted in second call, got %q", out2)
	}

	if got := countExtraTextPatternCacheEntries(); got != 1 {
		t.Fatalf("expected 1 cached pattern set, got %d", got)
	}
}

func TestRedactText_DefaultPathDoesNotUseExtraCache(t *testing.T) {
	clearExtraTextPatternCache()

	out := RedactText("access_token=abc")
	if !strings.Contains(out, "access_token=***") {
		t.Fatalf("expected default key redacted, got %q", out)
	}
	if got := countExtraTextPatternCacheEntries(); got != 0 {
		t.Fatalf("expected extra cache to remain empty, got %d", got)
	}
}

func clearExtraTextPatternCache() {
	extraTextPatternCache.Range(func(key, value any) bool {
		extraTextPatternCache.Delete(key)
		return true
	})
}

func countExtraTextPatternCacheEntries() int {
	count := 0
	extraTextPatternCache.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

func TestCredentialRedactionAcrossNestedBodiesAndURI(t *testing.T) {
	for _, input := range []string{
		`{"nested":[{"API_KEY":"credential-secret","Authorization":"Bearer credential-secret"}],"url":"https://internal.example/v1beta/models?key=credential-secret&alt=json"}`,
		`POST https://user:credential-secret@internal.example/v1beta/models?KEY=credential-secret&alt=json Authorization: Bearer credential-secret`,
		`https://internal.example/v1beta/models?%6bey=credential-secret&alt=json`,
		`{"api_key":"` + strings.Repeat("credential-secret", 1000),
	} {
		output := RedactText(input)
		if strings.Contains(output, "credential-secret") {
			t.Fatalf("credential leaked: %.80s", output)
		}
	}
	diagnostic := RedactDiagnostic("Quota exhausted for operator@example.internal at https://db.internal/private?key=credential-secret")
	if strings.Contains(diagnostic, "example.internal") || strings.Contains(diagnostic, "db.internal") || strings.Contains(diagnostic, "credential-secret") {
		t.Fatal(diagnostic)
	}
	if !strings.Contains(diagnostic, "Quota exhausted") {
		t.Fatal("useful upstream reason lost")
	}
}

func TestRedactNestedJSONAndProxyCredentials(t *testing.T) {
	const sensitive = "synthetic-redaction-marker"
	out := RedactMap(map[string]any{
		"embedded":           "{\"nested\":\"{\\\"privateKey\\\":\\\"" + sensitive + "\\\"}\",\"customSecret\":\"" + sensitive + "\"}",
		"serviceAccountJson": sensitive,
		"proxy":              "socks5h://proxy-fixture-user:" + sensitive + "@proxy.example:badport",
	}, "custom_secret")
	for _, value := range out {
		rendered, ok := value.(string)
		if !ok || strings.Contains(rendered, sensitive) || strings.Contains(rendered, "proxy-fixture-user") {
			t.Fatalf("credential survived redaction: %v", out)
		}
	}
}
