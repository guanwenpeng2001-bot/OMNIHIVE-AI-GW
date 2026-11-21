package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/urlvalidator"
	"golang.org/x/sync/singleflight"
)

type WorkBuddyOAuthStart struct {
	SessionID string `json:"session_id"`
	AuthURL   string `json:"auth_url"`
	State     string `json:"state"`
	Site      string `json:"site"`
	ExpiresIn int    `json:"expires_in"`
}
type WorkBuddyOAuthPoll struct {
	Status      string         `json:"status"`
	Site        string         `json:"site"`
	AccountName string         `json:"account_name,omitempty"`
	Credentials map[string]any `json:"credentials,omitempty"`
}
type workBuddySession struct {
	mu                          sync.Mutex
	state, site, proxy, binding string
	expires                     time.Time
	account                     *Account
	completed                   *WorkBuddyOAuthPoll
}
type workBuddyRenewed struct {
	credentials map[string]any
	expires     time.Time
}
type WorkBuddyOAuthService struct {
	cfg       *config.Config
	proxyRepo ProxyRepository
	mu        sync.Mutex
	sessions  map[string]*workBuddySession
	renewed   map[string]workBuddyRenewed
	refresh   singleflight.Group
	// Endpoint overrides are private and used only by local protocol tests.
	endpoints map[string]string
}

func NewWorkBuddyOAuthService(cfg *config.Config, proxyRepo ProxyRepository) *WorkBuddyOAuthService {
	return &WorkBuddyOAuthService{cfg: cfg, proxyRepo: proxyRepo, sessions: map[string]*workBuddySession{}, renewed: map[string]workBuddyRenewed{}}
}
func (s *WorkBuddyOAuthService) endpoint(a *Account) string {
	if s.endpoints != nil {
		if e := s.endpoints[workBuddySiteFor(a).ID]; e != "" {
			return e
		}
	}
	return workBuddyEndpoint(a)
}

type workBuddyAPIError struct{ Status, Code int }

func (e *workBuddyAPIError) Error() string {
	return fmt.Sprintf("WorkBuddy request rejected (HTTP %d, code %d)", e.Status, e.Code)
}
func (s *WorkBuddyOAuthService) call(ctx context.Context, a *Account, proxy, method, path string, body any, headers http.Header) (json.RawMessage, error) {
	if s == nil || s.cfg == nil {
		return nil, errors.New("WorkBuddy URL policy is not configured")
	}
	policy := s.cfg.Security.URLAllowlist
	endpoint := s.endpoint(a)
	if s.endpoints == nil && (strings.HasPrefix(path, "/v2/plugin/auth/state?") || strings.HasPrefix(path, "/v2/plugin/auth/token?") || strings.HasPrefix(path, "/v2/plugin/login/account?")) {
		endpoint = workBuddySiteFor(a).Endpoint
	}
	target, err := urlvalidator.ValidateHTTPURL(endpoint+path, policy.AllowInsecureHTTP, policy.UpstreamValidationOptions())
	if err != nil {
		return nil, fmt.Errorf("WorkBuddy URL not allowed: %w", err)
	}
	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	if a != nil && workBuddyToken(a) != "" {
		applyWorkBuddySign(req.Header, a)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "WorkBuddy/"+workBuddyVersion)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header[k] = v
	}
	client, err := httpclient.GetClient(httpclient.Options{Timeout: 20 * time.Second, ProxyURL: proxy, AllowPrivateHosts: policy.AllowPrivateHosts, PrivateHosts: policy.UpstreamHosts})
	if err != nil {
		return nil, err
	}
	local := *client
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := local.Do(req)
	if err != nil {
		return nil, errors.New("WorkBuddy transport failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 {
		return nil, errors.New("WorkBuddy response could not be read")
	}
	var env struct {
		Code json.Number     `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	parseErr := json.Unmarshal(raw, &env)
	code, _ := env.Code.Int64()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || code != 0 {
		return nil, &workBuddyAPIError{resp.StatusCode, int(code)}
	}
	if parseErr != nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, errors.New("WorkBuddy response has no data")
	}
	return env.Data, nil
}
func (s *WorkBuddyOAuthService) Start(ctx context.Context, site string, proxyID *int64, binding string) (*WorkBuddyOAuthStart, error) {
	a := &Account{Platform: PlatformWorkBuddy, Type: AccountTypeOAuth, Credentials: map[string]any{"site": NormalizeWorkBuddySite(site, "", "")}}
	proxy := ""
	if proxyID != nil {
		if *proxyID <= 0 || s.proxyRepo == nil {
			return nil, errors.New("invalid WorkBuddy proxy")
		}
		p, err := s.proxyRepo.GetByID(ctx, *proxyID)
		if err != nil {
			return nil, err
		}
		if p == nil {
			return nil, errors.New("WorkBuddy proxy unavailable")
		}
		proxy = p.URL()
	}
	h := http.Header{}
	for _, k := range []string{"X-No-Authorization", "X-No-User-Id", "X-No-Enterprise-Id", "X-No-Department-Info"} {
		h.Set(k, "true")
	}
	raw, err := s.call(ctx, a, proxy, http.MethodPost, "/v2/plugin/auth/state?platform="+workBuddySiteFor(a).Platform, map[string]any{}, h)
	if err != nil {
		return nil, err
	}
	var data struct {
		State   string `json:"state"`
		AuthURL string `json:"authUrl"`
	}
	if json.Unmarshal(raw, &data) != nil || data.State == "" {
		return nil, errors.New("WorkBuddy returned no login state")
	}
	u, err := url.Parse(data.AuthURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("WorkBuddy returned an invalid login URL")
	}
	q := u.Query()
	q.Set("version", workBuddyVersion)
	q.Set("loginSessionId", workBuddyHex())
	u.RawQuery = q.Encode()
	id := workBuddyHex()
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if now.After(v.expires) {
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= 1000 {
		return nil, errors.New("WorkBuddy login capacity reached")
	}
	s.sessions[id] = &workBuddySession{state: data.State, site: workBuddySiteFor(a).ID, proxy: proxy, binding: binding, expires: now.Add(5 * time.Minute), account: a}
	return &WorkBuddyOAuthStart{id, u.String(), data.State, workBuddySiteFor(a).ID, 300}, nil
}
func workBuddyMergeTokens(old map[string]any, raw json.RawMessage) (map[string]any, error) {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	access, _ := data["accessToken"].(string)
	if strings.TrimSpace(access) == "" {
		return nil, errors.New("WorkBuddy returned no access token")
	}
	out := MergeCredentials(old, map[string]any{"access_token": access})
	for from, to := range map[string]string{"refreshToken": "refresh_token", "domain": "domain", "tokenType": "token_type"} {
		if v, ok := data[from].(string); ok && v != "" {
			out[to] = v
		}
	}
	for _, pair := range [][3]string{{"expiresAt", "expiresIn", "expires_at"}, {"refreshExpiresAt", "refreshExpiresIn", "refresh_expires_at"}} {
		t := workBuddyTime(data[pair[0]])
		if t == nil {
			if n, ok := data[pair[1]].(float64); ok && n > 0 {
				v := time.Now().Add(time.Duration(n) * time.Second)
				t = &v
			}
		}
		if t != nil {
			out[pair[2]] = t.UTC().Format(time.RFC3339Nano)
		}
	}
	return out, nil
}
func (s *WorkBuddyOAuthService) Poll(ctx context.Context, id, binding string) (*WorkBuddyOAuthPoll, error) {
	s.mu.Lock()
	session := s.sessions[id]
	s.mu.Unlock()
	if session == nil || time.Now().After(session.expires) || session.binding != binding {
		return nil, errors.New("WorkBuddy login expired; start again")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.completed != nil {
		return session.completed, nil
	}
	pending := &WorkBuddyOAuthPoll{Status: "pending", Site: session.site}
	a := session.account
	q := url.QueryEscape(session.state)
	if workBuddyToken(a) == "" {
		raw, err := s.call(ctx, a, session.proxy, http.MethodGet, "/v2/plugin/auth/token?state="+q, nil, nil)
		var apiErr *workBuddyAPIError
		if errors.As(err, &apiErr) && apiErr.Code == 11217 {
			return pending, nil
		}
		if err != nil {
			return nil, err
		}
		creds, err := workBuddyMergeTokens(a.Credentials, raw)
		if err != nil {
			return nil, err
		}
		a.Credentials = creds
	}
	h := http.Header{}
	h.Set("X-No-User-Id", "true")
	h.Set("X-No-Enterprise-Id", "true")
	raw, err := s.call(ctx, a, session.proxy, http.MethodGet, "/v2/plugin/login/account?state="+q, nil, h)
	var apiErr *workBuddyAPIError
	if errors.As(err, &apiErr) && apiErr.Code == 12151 {
		return pending, nil
	}
	if err != nil {
		return nil, err
	}
	var who struct {
		UID            string `json:"uid"`
		Nickname       string `json:"nickname"`
		Phone          string `json:"phoneNumber"`
		Type           string `json:"type"`
		EnterpriseID   string `json:"enterpriseId"`
		EnterpriseName string `json:"enterpriseName"`
		LastLogin      bool   `json:"lastLogin"`
	}
	if json.Unmarshal(raw, &who) != nil || who.UID == "" {
		return nil, errors.New("WorkBuddy returned no account identity")
	}
	a.Credentials["uid"] = who.UID
	if who.EnterpriseID == "" && who.Type != "" && who.Type != "personal" || who.EnterpriseID != "" && who.EnterpriseName == "" {
		if entries, e := s.call(ctx, a, session.proxy, http.MethodGet, "/v2/plugin/accounts", nil, nil); e == nil {
			var list struct {
				Accounts []json.RawMessage `json:"accounts"`
			}
			_ = json.Unmarshal(entries, &list)
			var mine, same, last []json.RawMessage
			for _, entry := range list.Accounts {
				v := who
				v.EnterpriseID = ""
				if json.Unmarshal(entry, &v) != nil || v.UID != who.UID || v.EnterpriseID == "" {
					continue
				}
				mine = append(mine, entry)
				if v.Type == who.Type {
					same = append(same, entry)
				}
				if v.LastLogin {
					last = append(last, entry)
				}
				if who.EnterpriseID != "" && v.EnterpriseID == who.EnterpriseID {
					who.EnterpriseName = v.EnterpriseName
				}
			}
			if who.EnterpriseID == "" {
				var pick json.RawMessage
				if len(same) == 1 {
					pick = same[0]
				} else if len(last) == 1 {
					pick = last[0]
				} else if len(mine) == 1 {
					pick = mine[0]
				}
				if pick != nil {
					v := who
					_ = json.Unmarshal(pick, &v)
					who.EnterpriseID = v.EnterpriseID
					who.EnterpriseName = v.EnterpriseName
				}
			}
		}
	}
	a.Credentials["enterprise_id"] = who.EnterpriseID
	if a.GetCredential("domain") == "" {
		a.Credentials["domain"] = workBuddySiteFor(a).Domain
	}
	name := firstNonEmpty(who.Nickname, who.Phone, who.UID)
	if who.EnterpriseID != "" {
		team := who.EnterpriseName
		if team == "" {
			tail := who.EnterpriseID
			if len(tail) > 6 {
				tail = tail[len(tail)-6:]
			}
			team = "enterprise " + tail
		}
		name += " · " + team
	}
	session.completed = &WorkBuddyOAuthPoll{Status: "completed", Site: session.site, AccountName: name, Credentials: MergeCredentials(nil, a.Credentials)}
	return session.completed, nil
}
func (s *WorkBuddyOAuthService) RefreshAccountToken(ctx context.Context, a *Account) (map[string]any, error) {
	refresh := a.GetCredential("refresh_token")
	if refresh == "" {
		return nil, errors.New("WorkBuddy refresh token is missing")
	}
	if t := workBuddyTime(a.Credentials["refresh_expires_at"]); t != nil && time.Now().After(*t) {
		return nil, errors.New("WorkBuddy refresh token expired")
	}
	digest := sha256.Sum256([]byte(s.endpoint(a) + "|" + refresh))
	key := hex.EncodeToString(digest[:])
	v, err, _ := s.refresh.Do(key, func() (any, error) {
		s.mu.Lock()
		cached, ok := s.renewed[key]
		now := time.Now()
		for k, c := range s.renewed {
			if now.After(c.expires) {
				delete(s.renewed, k)
			}
		}
		s.mu.Unlock()
		if ok && now.Before(cached.expires) {
			return cached.credentials, nil
		}
		proxy, e := a.EgressProxyURL(ctx)
		if e != nil {
			return nil, e
		}
		h := http.Header{}
		h.Set("X-Refresh-Token", refresh)
		h.Set("X-Auth-Refresh-Source", "plugin")
		raw, e := s.call(ctx, a, proxy, http.MethodPost, "/v2/plugin/auth/token/refresh", map[string]any{}, h)
		if e != nil {
			return nil, e
		}
		creds, e := workBuddyMergeTokens(nil, raw)
		if e != nil {
			return nil, e
		}
		s.mu.Lock()
		s.renewed[key] = workBuddyRenewed{creds, time.Now().Add(time.Minute)}
		s.mu.Unlock()
		return creds, nil
	})
	if err != nil {
		return nil, err
	}
	return MergeCredentials(a.Credentials, v.(map[string]any)), nil
}

type WorkBuddyTokenRefresher struct{ service *WorkBuddyOAuthService }

func NewWorkBuddyTokenRefresher(s *WorkBuddyOAuthService) *WorkBuddyTokenRefresher {
	return &WorkBuddyTokenRefresher{s}
}
func (r *WorkBuddyTokenRefresher) CanRefresh(a *Account) bool {
	return a != nil && a.Platform == PlatformWorkBuddy && a.Type == AccountTypeOAuth && a.GetCredential("refresh_token") != ""
}
func (r *WorkBuddyTokenRefresher) NeedsRefresh(a *Account, window time.Duration) bool {
	if !r.CanRefresh(a) {
		return false
	}
	t := workBuddyTime(a.Credentials["expires_at"])
	return workBuddyToken(a) == "" || t == nil || time.Until(*t) < window
}
func (r *WorkBuddyTokenRefresher) Refresh(ctx context.Context, a *Account) (map[string]any, error) {
	return r.service.RefreshAccountToken(ctx, a)
}
func (r *WorkBuddyTokenRefresher) CacheKey(a *Account) string {
	sum := sha256.Sum256([]byte(workBuddyEndpoint(a) + "|" + a.GetCredential("refresh_token")))
	return "workbuddy:" + hex.EncodeToString(sum[:])
}
func (s *TokenRefreshService) SetWorkBuddyOAuthService(oauth *WorkBuddyOAuthService) {
	r := NewWorkBuddyTokenRefresher(oauth)
	s.registrations = append(s.registrations, tokenRefreshRegistration{platform: PlatformWorkBuddy, refresher: r, executor: r})
}
