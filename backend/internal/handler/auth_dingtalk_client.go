package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// dingTalkClientConfig 是 DingTalkClient 需要的最小配置子集
type dingTalkClientConfig struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	UserInfoURL  string
}

type DingTalkClient struct {
	cfg         dingTalkClientConfig
	appToken    string
	appTokenExp time.Time // 钉钉 7200s，留 200s 余量 → 7000s
	mu          sync.Mutex
	httpClient  *http.Client
	redisClient *redis.Client
}

type DingTalkUserTokenResp struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpireIn     int64  `json:"expireIn"`
	CorpID       string `json:"corpId"`
}

func (c *DingTalkClient) ExchangeCodeForUserToken(ctx context.Context, code string) (*DingTalkUserTokenResp, error) {
	body := map[string]string{
		"clientId":     c.cfg.ClientID,
		"clientSecret": c.cfg.ClientSecret,
		"code":         code,
		"grantType":    "authorization_code",
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, parseDingTalkErr(raw, resp.StatusCode)
	}
	var out DingTalkUserTokenResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, parseDingTalkErr(raw, resp.StatusCode)
	}
	return &out, nil
}

type DingTalkAPIError struct {
	Code    string
	Message string
	HTTP    int
}

func (e *DingTalkAPIError) Error() string {
	return fmt.Sprintf("dingtalk api error code=%s msg=%s http=%d", e.Code, e.Message, e.HTTP)
}

func parseDingTalkErr(raw []byte, status int) error {
	var v struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	_ = json.Unmarshal(raw, &v)
	code := v.Code
	if code == "" && v.ErrCode != 0 {
		code = fmt.Sprintf("%d", v.ErrCode)
	}
	msg := v.Message
	if msg == "" {
		msg = v.ErrMsg
	}
	return &DingTalkAPIError{Code: code, Message: msg, HTTP: status}
}

// GetUnionIdByUserToken 调用 /v1.0/contact/users/me 返回 unionId 与用户自设昵称 nick。
// nick 来自钉钉新版 OIDC 接口（用户在 App 个人资料填的昵称），与旧版 user/get.nickname 不同源。
func (c *DingTalkClient) GetUnionIdByUserToken(ctx context.Context, userToken string) (unionID string, nick string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.UserInfoURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("x-acs-dingtalk-access-token", userToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", parseDingTalkErr(raw, resp.StatusCode)
	}
	var v struct {
		UnionID string `json:"unionId"`
		Nick    string `json:"nick"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(v.UnionID) == "" {
		return "", "", parseDingTalkErr(raw, resp.StatusCode)
	}
	return v.UnionID, v.Nick, nil
}

type DingTalkStaffInfo struct {
	UserID   string
	Name     string // 企业内真实姓名（钉钉企业管理后台配置）
	Nickname string // 钉钉个人昵称（用户自己设置）
	Email    string
	DeptIDs  []int64
	// CorpID 不来自 staff 接口，来自 userToken；不在此 struct
}

// dingTalkOAPIBase 推导钉钉旧版 OAPI base URL（host: api.dingtalk.com → oapi.dingtalk.com）。
// getbyunionid 与 topapi/v2/user/get 仅在旧版 OAPI 提供，不在 v1.0 OpenAPI。
func (c *DingTalkClient) dingTalkOAPIBase() string {
	u, err := url.Parse(c.cfg.UserInfoURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "https://oapi.dingtalk.com"
	}
	host := u.Host
	if strings.HasPrefix(host, "api.") {
		host = "oapi." + strings.TrimPrefix(host, "api.")
	}
	return u.Scheme + "://" + host
}

func (c *DingTalkClient) GetAppToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.redisClient != nil {
		return c.getSharedAppToken(ctx)
	}
	if c.appToken != "" && time.Now().Before(c.appTokenExp) {
		return c.appToken, nil
	}
	token, ttl, err := c.fetchAppToken(ctx)
	if err != nil {
		return "", err
	}
	c.appToken, c.appTokenExp = token, time.Now().Add(ttl)
	return token, nil
}

func (c *DingTalkClient) fetchAppToken(ctx context.Context) (string, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body := map[string]string{"appKey": c.cfg.ClientID, "appSecret": c.cfg.ClientSecret}
	payload, _ := json.Marshal(body)
	// 钉钉新版 v1.0 企业内部应用 access_token: POST /v1.0/oauth2/accessToken
	// 此 token 也可作为旧版 OAPI 的 access_token 使用（钉钉文档已说明）
	appTokenURL := strings.Replace(c.cfg.TokenURL, "/oauth2/userAccessToken", "/oauth2/accessToken", 1)
	if !strings.Contains(appTokenURL, "accessToken") && !strings.Contains(appTokenURL, "gettoken") {
		appTokenURL = c.cfg.TokenURL // fallback for test stub
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, appTokenURL, bytes.NewReader(payload))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", 0, parseDingTalkErr(raw, resp.StatusCode)
	}
	var v struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int64  `json:"expireIn"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", 0, err
	}
	if v.AccessToken == "" {
		return "", 0, parseDingTalkErr(raw, resp.StatusCode)
	}
	ttl := v.ExpireIn
	if ttl > 200 {
		ttl -= 200
	}
	if ttl <= 0 {
		return "", 0, fmt.Errorf("dingtalk token has invalid expiry")
	}
	return v.AccessToken, time.Duration(ttl) * time.Second, nil
}

func (c *DingTalkClient) GetUserIdByUnionId(ctx context.Context, unionID string) (string, error) {
	appToken, err := c.GetAppToken(ctx)
	if err != nil {
		return "", err
	}
	body := map[string]string{"unionid": unionID}
	payload, _ := json.Marshal(body)
	// 钉钉旧版 OAPI: POST https://oapi.dingtalk.com/topapi/user/getbyunionid?access_token=XXX
	// access_token 通过 query string 传递（不是 header）
	var targetURL string
	if strings.Contains(c.cfg.UserInfoURL, "/contact/users/me") {
		targetURL = c.dingTalkOAPIBase() + "/topapi/user/getbyunionid?access_token=" + url.QueryEscape(appToken)
	} else {
		targetURL = c.cfg.UserInfoURL // fallback for test stub
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", parseDingTalkErr(raw, resp.StatusCode)
	}
	var v struct {
		Result struct {
			UserID string `json:"userid"`
		} `json:"result"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	if v.ErrCode != 0 {
		return "", parseDingTalkErr(raw, resp.StatusCode)
	}
	if strings.TrimSpace(v.Result.UserID) == "" {
		return "", parseDingTalkErr(raw, resp.StatusCode)
	}
	return v.Result.UserID, nil
}

// DingTalkDeptInfo 部门信息（topapi/v2/department/get 返回子集）
type DingTalkDeptInfo struct {
	DeptID   int64
	Name     string
	ParentID int64
}

// GetDeptInfo 查询单个部门信息（用于递归拼部门路径）。
// 调用钉钉旧版 OAPI: POST /topapi/v2/department/get?access_token=XXX
func (c *DingTalkClient) GetDeptInfo(ctx context.Context, deptID int64) (*DingTalkDeptInfo, error) {
	appToken, err := c.GetAppToken(ctx)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"dept_id": deptID, "language": "zh_CN"}
	payload, _ := json.Marshal(body)
	var targetURL string
	if strings.Contains(c.cfg.UserInfoURL, "/contact/users/me") {
		targetURL = c.dingTalkOAPIBase() + "/topapi/v2/department/get?access_token=" + url.QueryEscape(appToken)
	} else {
		targetURL = c.cfg.UserInfoURL // test stub fallback
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, parseDingTalkErr(raw, resp.StatusCode)
	}
	var v struct {
		Result struct {
			DeptID   int64  `json:"dept_id"`
			Name     string `json:"name"`
			ParentID int64  `json:"parent_id"`
		} `json:"result"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if v.ErrCode != 0 {
		return nil, parseDingTalkErr(raw, resp.StatusCode)
	}
	return &DingTalkDeptInfo{
		DeptID:   v.Result.DeptID,
		Name:     v.Result.Name,
		ParentID: v.Result.ParentID,
	}, nil
}

func (c *DingTalkClient) GetStaffInfoByUserId(ctx context.Context, userID string) (*DingTalkStaffInfo, error) {
	appToken, err := c.GetAppToken(ctx)
	if err != nil {
		return nil, err
	}
	body := map[string]string{"userid": userID}
	payload, _ := json.Marshal(body)
	// 钉钉旧版 OAPI: POST https://oapi.dingtalk.com/topapi/v2/user/get?access_token=XXX
	var targetURL string
	if strings.Contains(c.cfg.UserInfoURL, "/contact/users/me") {
		targetURL = c.dingTalkOAPIBase() + "/topapi/v2/user/get?access_token=" + url.QueryEscape(appToken)
	} else {
		targetURL = c.cfg.UserInfoURL // fallback for test stub
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, parseDingTalkErr(raw, resp.StatusCode)
	}
	var v struct {
		Result struct {
			UserID    string  `json:"userid"`
			Name      string  `json:"name"`
			Nickname  string  `json:"nickname"`
			Email     string  `json:"email"`
			OrgEmail  string  `json:"org_email"`
			Extension string  `json:"extension"`
			DeptID    []int64 `json:"dept_id_list"`
		} `json:"result"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if v.ErrCode != 0 {
		return nil, parseDingTalkErr(raw, resp.StatusCode)
	}
	if strings.TrimSpace(v.Result.UserID) == "" {
		return nil, parseDingTalkErr(raw, resp.StatusCode)
	}
	// 邮箱三级 fallback：org_email > email > extension["企业邮箱"]（钉钉自定义扩展字段，JSON string）
	email := strings.TrimSpace(v.Result.OrgEmail)
	emailSource := "org_email"
	if email == "" {
		email = strings.TrimSpace(v.Result.Email)
		emailSource = "email"
	}
	extensionParsed := false
	if email == "" && strings.TrimSpace(v.Result.Extension) != "" {
		var ext map[string]string
		if err := json.Unmarshal([]byte(v.Result.Extension), &ext); err == nil {
			extensionParsed = true
			if v, ok := ext["企业邮箱"]; ok {
				email = strings.TrimSpace(v)
				emailSource = "extension.企业邮箱"
			}
		}
	}
	if email == "" {
		emailSource = "none"
	}
	slog.Info("dingtalk staff fetched",
		"userid", v.Result.UserID,
		"name_present", v.Result.Name != "",
		"nickname_present", v.Result.Nickname != "",
		"name_eq_nickname", v.Result.Name != "" && v.Result.Name == v.Result.Nickname,
		"email_present", v.Result.Email != "",
		"org_email_present", v.Result.OrgEmail != "",
		"extension_present", v.Result.Extension != "",
		"extension_parsed", extensionParsed,
		"email_source", emailSource,
		"dept_count", len(v.Result.DeptID),
	)
	return &DingTalkStaffInfo{
		UserID:   v.Result.UserID,
		Name:     v.Result.Name,
		Nickname: v.Result.Nickname,
		Email:    email,
		DeptIDs:  v.Result.DeptID,
	}, nil
}

func (c *DingTalkClient) appTokenCacheKey() string {
	identity, _ := json.Marshal(c.cfg)
	return fmt.Sprintf("dingtalk:app-token:%x", sha256.Sum256(identity))
}

// Redis is authoritative when available; a failed cache operation falls back to
// an uncached upstream fetch rather than preserving a process-local stale token.
func (c *DingTalkClient) getSharedAppToken(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	key := c.appTokenCacheKey()
	lockKey := key + ":refresh"
	owner := uuid.NewString()
	fallback := func() (string, error) { token, _, err := c.fetchAppToken(ctx); return token, err }
	for {
		token, err := c.redisClient.Get(ctx, key).Result()
		if err == nil && token != "" {
			return token, nil
		}
		if err != nil && !errors.Is(err, redis.Nil) {
			return fallback()
		}
		acquired, err := c.redisClient.SetNX(ctx, lockKey, owner, 30*time.Second).Result()
		if err != nil {
			return fallback()
		}
		if acquired {
			defer func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
				defer cleanupCancel()
				_ = c.redisClient.Eval(cleanupCtx, `if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end return 0`, []string{lockKey}, owner).Err()
			}()
			// Another instance may have populated the cache between GET and SET NX.
			token, err = c.redisClient.Get(ctx, key).Result()
			if err == nil && token != "" {
				return token, nil
			}
			token, ttl, err := c.fetchAppToken(ctx)
			if err != nil {
				return "", err
			}
			// A timed-out owner must never overwrite a newer owner's token.
			// Cache publication is best effort after a successful upstream fetch.
			published, publishErr := c.redisClient.Eval(ctx, `if redis.call('GET', KEYS[1]) == ARGV[1] then redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3]); return 1 end return 0`, []string{lockKey, key}, owner, token, ttl.Milliseconds()).Int64()
			if publishErr == nil && published == 0 {
				current, readErr := c.redisClient.Get(ctx, key).Result()
				if readErr == nil && current != "" {
					return current, nil
				}
				return "", fmt.Errorf("dingtalk token refresh ownership expired")
			}
			return token, nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}

// ProvideAuthHandler supplies the shared Redis client in production.
func ProvideAuthHandler(cfg *config.Config, authService *service.AuthService, userService *service.UserService, settingService *service.SettingService, promoService *service.PromoService, redeemService *service.RedeemService, totpService *service.TotpService, userAttributeService *service.UserAttributeService, redisClient *redis.Client) *AuthHandler {
	handler := NewAuthHandler(cfg, authService, userService, settingService, promoService, redeemService, totpService, userAttributeService)
	handler.redisClient = redisClient
	return handler
}
