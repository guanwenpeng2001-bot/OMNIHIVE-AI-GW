package urlvalidator

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestValidateURLFormat(t *testing.T) {
	if _, err := ValidateURLFormat("", false); err == nil {
		t.Fatalf("expected empty url to fail")
	}
	if _, err := ValidateURLFormat("://bad", false); err == nil {
		t.Fatalf("expected invalid url to fail")
	}
	if _, err := ValidateURLFormat("http://example.com", false); err == nil {
		t.Fatalf("expected http to fail when allow_insecure_http is false")
	}
	if _, err := ValidateURLFormat("https://example.com", false); err != nil {
		t.Fatalf("expected https to pass, got %v", err)
	}
	if _, err := ValidateURLFormat("http://example.com", true); err != nil {
		t.Fatalf("expected http to pass when allow_insecure_http is true, got %v", err)
	}
	if _, err := ValidateURLFormat("https://example.com:bad", true); err == nil {
		t.Fatalf("expected invalid port to fail")
	}

	// 验证末尾斜杠被移除
	normalized, err := ValidateURLFormat("https://example.com/", false)
	if err != nil {
		t.Fatalf("expected trailing slash url to pass, got %v", err)
	}
	if normalized != "https://example.com" {
		t.Fatalf("expected trailing slash to be removed, got %s", normalized)
	}

	// 验证多个末尾斜杠被移除
	normalized, err = ValidateURLFormat("https://example.com///", false)
	if err != nil {
		t.Fatalf("expected multiple trailing slashes to pass, got %v", err)
	}
	if normalized != "https://example.com" {
		t.Fatalf("expected all trailing slashes to be removed, got %s", normalized)
	}

	// 验证带路径的 URL 末尾斜杠被移除
	normalized, err = ValidateURLFormat("https://example.com/api/v1/", false)
	if err != nil {
		t.Fatalf("expected trailing slash url with path to pass, got %v", err)
	}
	if normalized != "https://example.com/api/v1" {
		t.Fatalf("expected trailing slash to be removed from path, got %s", normalized)
	}
}

func TestValidateHTTPURL(t *testing.T) {
	if _, err := ValidateHTTPURL("http://example.com", false, ValidationOptions{}); err == nil {
		t.Fatalf("expected http to fail when allow_insecure_http is false")
	}
	if _, err := ValidateHTTPURL("http://example.com", true, ValidationOptions{}); err != nil {
		t.Fatalf("expected http to pass when allow_insecure_http is true, got %v", err)
	}
	if _, err := ValidateHTTPURL("https://example.com", false, ValidationOptions{RequireAllowlist: true}); err == nil {
		t.Fatalf("expected require allowlist to fail when empty")
	}
	if _, err := ValidateHTTPURL("https://example.com", false, ValidationOptions{AllowedHosts: []string{"api.example.com"}}); err == nil {
		t.Fatalf("expected host not in allowlist to fail")
	}
	if _, err := ValidateHTTPURL("https://api.example.com", false, ValidationOptions{AllowedHosts: []string{"api.example.com"}}); err != nil {
		t.Fatalf("expected allowlisted host to pass, got %v", err)
	}
	if _, err := ValidateHTTPURL("https://sub.api.example.com", false, ValidationOptions{AllowedHosts: []string{"*.example.com"}}); err != nil {
		t.Fatalf("expected wildcard allowlist to pass, got %v", err)
	}
	if _, err := ValidateHTTPURL("https://localhost", false, ValidationOptions{AllowPrivate: false}); err == nil {
		t.Fatalf("expected localhost to be blocked when allow_private_hosts is false")
	}
}

func TestIsBlockedHost(t *testing.T) {
	for _, host := range []string{
		"localhost", "LOCALHOST", "foo.localhost", " 127.0.0.1 ",
		"127.0.0.1", "::1", "10.0.0.5", "172.16.0.1", "192.168.1.1",
		"169.254.169.254", "0.0.0.0", "::", "fe80::1", "fc00::1", "::ffff:127.0.0.1",
	} {
		if !IsBlockedHost(host) {
			t.Fatalf("expected %q to be blocked", host)
		}
	}
	for _, host := range []string{"example.com", "cdn.example.com", "93.184.216.34", "8.8.8.8", "2606:4700:4700::1111", ""} {
		if IsBlockedHost(host) {
			t.Fatalf("expected %q to be allowed", host)
		}
	}
}

func TestValidateResolvedIPRespectsCallerContext(t *testing.T) {
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	defer func() { net.DefaultResolver = previous }()
	for _, canceled := range []bool{true, false} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		if canceled {
			cancel()
		}
		started := time.Now()
		err := ValidateResolvedIP(ctx, "budget.example")
		cancel()
		if err == nil || time.Since(started) > 500*time.Millisecond {
			t.Fatalf("lookup escaped caller context: err=%v elapsed=%s", err, time.Since(started))
		}
	}
}

func TestRejectAmbiguousNumericHost(t *testing.T) {
	for _, host := range []string{"2130706433", "0177.0.0.1", "127.1", "0x7f000001", "08.08.08.08", "169.254.169.254", "::1", "fd00:ec2::254"} {
		if !IsBlockedHost(host) {
			t.Errorf("expected blocked host %q", host)
		}
	}
}
