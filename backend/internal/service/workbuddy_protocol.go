package service

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const workBuddyVersion = "5.9.3"
const workBuddyCLI = "2.165.0"

type workBuddySite struct{ ID, Endpoint, Domain, Platform, AppName string }

// NormalizeWorkBuddySite accepts the desktop names used by imported credentials.
func NormalizeWorkBuddySite(site, domain, baseURL string) string {
	switch strings.ToLower(strings.TrimSpace(site)) {
	case "cn", "china", "workbuddy":
		return "cn"
	case "global", "intl", "international", "workbuddy-ai", "workbuddy_ai":
		return "global"
	}
	for _, raw := range []string{domain, baseURL} {
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		u, err := url.Parse(raw)
		if err == nil && (strings.EqualFold(u.Hostname(), "workbuddy.ai") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".workbuddy.ai")) {
			return "global"
		}
	}
	return "cn"
}
func workBuddySiteFor(a *Account) workBuddySite {
	site := "cn"
	if a != nil {
		site = NormalizeWorkBuddySite(a.GetCredential("site"), a.GetCredential("domain"), a.GetCredential("base_url"))
	}
	if site == "global" {
		return workBuddySite{site, "https://www.workbuddy.ai", "www.workbuddy.ai", "workbuddy-ai", "workbuddy-ai"}
	}
	return workBuddySite{site, "https://copilot.tencent.com", "www.workbuddy.cn", "workbuddy", "WorkBuddy"}
}

var workBuddyEnterpriseHost = regexp.MustCompile(`^[a-z0-9-]+\.(sso\.copilot\.tencent\.com|copilot\.qq\.com|sso\.codebuddy\.cn)$`)

func workBuddyEndpoint(a *Account) string {
	site := workBuddySiteFor(a)
	if a == nil {
		return site.Endpoint
	}
	if base := strings.TrimRight(strings.TrimSpace(a.GetCredential("base_url")), "/"); base != "" {
		// Credentials accept either the endpoint or the documented chat base.
		return strings.TrimSuffix(strings.TrimSuffix(base, "/chat/completions"), "/v2")
	}
	d := strings.ToLower(strings.TrimSpace(a.GetCredential("domain")))
	if a.Type != AccountTypeAPIKey && workBuddyEnterpriseHost.MatchString(d) {
		if d == "tencent.sso.copilot.tencent.com" || d == "tencent.sso.codebuddy.cn" {
			return "https://copilot.tencent.com"
		}
		return "https://" + d
	}
	return site.Endpoint
}
func workBuddyToken(a *Account) string {
	if a.Type == AccountTypeAPIKey {
		return strings.TrimSpace(a.GetCredential("api_key"))
	}
	if a.Type == AccountTypeOAuth {
		return strings.TrimSpace(a.GetCredential("access_token"))
	}
	return ""
}
func workBuddyTime(v any) *time.Time {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return &t
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 253402300799000 {
		return nil
	}
	if n >= 1e11 {
		n /= 1000
	}
	t := time.Unix(int64(n), int64((n-float64(int64(n)))*1e9)).UTC()
	return &t
}
func workBuddyHex() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

var workBuddyIDSalt = workBuddyHex()

func workBuddyID(kind, value string) string {
	sum := sha256.Sum256([]byte(workBuddyIDSalt + "|" + kind + "|" + value))
	return hex.EncodeToString(sum[:16])
}
func applyWorkBuddySign(h http.Header, a *Account) {
	site := workBuddySiteFor(a)
	h.Set("Authorization", "Bearer "+workBuddyToken(a))
	h.Del("X-API-Key")
	h.Del("X-Enterprise-Id")
	h.Del("X-Tenant-Id")
	if a.Type == AccountTypeAPIKey {
		key := workBuddyToken(a)
		tail := key
		if len(tail) > 8 {
			tail = tail[len(tail)-8:]
		}
		h.Set("X-API-Key", key)
		h.Set("X-User-Id", "anonymous_"+tail)
		h.Del("X-Domain")
	} else {
		h.Set("X-User-Id", a.GetCredential("uid"))
		d := strings.TrimSpace(a.GetCredential("domain"))
		if d == "" {
			d = site.Domain
		}
		h.Set("X-Domain", d)
		if id := strings.TrimSpace(a.GetCredential("enterprise_id")); id != "" {
			h.Set("X-Enterprise-Id", id)
			h.Set("X-Tenant-Id", id)
		}
	}
	for k, v := range map[string]string{"X-Product": "SaaS", "X-IDE-Type": "WorkBuddy", "X-IDE-Name": "WorkBuddy", "X-IDE-Version": workBuddyVersion, "X-Agent-Purpose": "conversation", "X-Agent-Intent": "craft", "X-Requested-With": "XMLHttpRequest", "User-Agent": "WorkBuddy/" + workBuddyVersion + " " + site.AppName + "/" + workBuddyVersion + " CLI/" + workBuddyCLI} {
		h.Set(k, v)
	}
}
func applyWorkBuddyChatHeaders(h http.Header, a *Account, body []byte) {
	applyWorkBuddySign(h, a)
	osName := map[string]string{"linux": "Linux", "darwin": "MacOS", "windows": "Windows", "freebsd": "FreeBSD", "openbsd": "OpenBSD"}[runtime.GOOS]
	if osName == "" {
		osName = "Other:" + runtime.GOOS
	}
	arch := map[string]string{"amd64": "x64", "386": "x32", "arm64": "arm64", "arm": "arm"}[runtime.GOARCH]
	if arch == "" {
		arch = "other:" + runtime.GOARCH
	}
	for k, v := range map[string]string{"Accept": "application/json", "X-Agent-Type": "main", "X-Agent-Runtime": "local-agent", "X-Stainless-Retry-Count": "0", "X-Stainless-Timeout": "1200", "X-Stainless-Lang": "js", "X-Stainless-Package-Version": "6.25.0", "X-Stainless-OS": osName, "X-Stainless-Arch": arch, "X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v22.21.1"} {
		h.Set(k, v)
	}
	// Derive stable conversation/turn identities from user messages, scoped to
	// the selected account. Tool continuations and retries retain the turn ID.

	conv, turn := workBuddyConversationKeys(body)
	scope := workBuddySiteFor(a).ID + "|" + a.GetCredential("uid") + "|" + a.GetCredential("enterprise_id")
	if a.Type == AccountTypeAPIKey {
		scope += "|" + workBuddyToken(a)
	}
	if h.Get("X-Conversation-ID") == "" && conv != "" {
		h.Set("X-Conversation-ID", workBuddyID("conversation", scope+"|"+workBuddyID("conversation", conv)))
	}
	if h.Get("X-Conversation-Request-ID") == "" {
		id := workBuddyHex()
		if turn != "" {
			id = workBuddyID("turn", scope+"|"+workBuddyID("turn", conv+"|"+turn))
		}
		h.Set("X-Conversation-Request-ID", id)
	}
	msg := workBuddyHex()
	h.Set("X-Conversation-Message-ID", msg)
	h.Set("X-Request-ID", msg)
}

func normalizeWorkBuddyAccount(a *Account) error {
	if a.Type != AccountTypeOAuth && a.Type != AccountTypeAPIKey {
		return fmt.Errorf("WorkBuddy supports oauth and apikey accounts")
	}
	a.Credentials = MergeCredentials(nil, a.Credentials)
	a.Credentials["site"] = workBuddySiteFor(a).ID
	for _, key := range []string{"expires_at", "refresh_expires_at"} {
		if value, ok := a.Credentials[key]; ok && value != nil && value != "" {
			t := workBuddyTime(value)
			if t == nil {
				return fmt.Errorf("invalid WorkBuddy %s", key)
			}
			a.Credentials[key] = t.UTC().Format(time.RFC3339Nano)
		}
	}
	return nil
}

type workBuddyIDMessage struct {
	Role      string            `json:"role"`
	Content   json.RawMessage   `json:"content"`
	ToolCalls []json.RawMessage `json:"tool_calls"`
}

func workBuddyContentSignature(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out strings.Builder
	for _, raw := range parts {
		var part struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &part) != nil {
			continue
		}
		switch part.Type {
		case "", "text":
			out.WriteString(part.Text)
		case "tool_result", "tool-result":
			continue
		default:
			var buf bytes.Buffer
			if json.Compact(&buf, raw) != nil {
				continue
			}
			sum := sha256.Sum256(buf.Bytes())
			out.WriteString("[" + part.Type + ":" + hex.EncodeToString(sum[:4]) + "]")
		}
	}
	return out.String()
}
func workBuddyHasBlock(m workBuddyIDMessage, kind string) bool {
	var parts []struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(m.Content, &parts)
	for _, part := range parts {
		if part.Type == kind {
			return true
		}
	}
	return false
}
func workBuddyConversationKeys(body []byte) (conv, turn string) {
	var chat struct {
		Messages []workBuddyIDMessage `json:"messages"`
	}
	_ = json.Unmarshal(body, &chat)
	for i, m := range chat.Messages {
		if m.Role != "user" {
			continue
		}
		signature := workBuddyContentSignature(m.Content)
		if conv == "" && signature != "" {
			conv = "first:" + signature
		}
		if workBuddyHasBlock(m, "tool_result") {
			continue
		}
		j := i - 1
		for j >= 0 && chat.Messages[j].Role == "system" {
			j--
		}
		if j >= 0 {
			prev := chat.Messages[j]
			if prev.Role == "tool" || (prev.Role == "user" && workBuddyHasBlock(prev, "tool_result")) || (prev.Role == "assistant" && (len(prev.ToolCalls) > 0 || workBuddyHasBlock(prev, "tool_use"))) {
				continue
			}
		}
		turn = ""
		if signature != "" {
			turn = fmt.Sprintf("u%d:%s", i, signature)
		}
	}
	return
}
