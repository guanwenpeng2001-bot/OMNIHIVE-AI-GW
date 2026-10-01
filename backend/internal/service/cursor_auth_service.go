package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
)

const cursorAuthorizationLifetime = 10 * time.Minute
const cursorAuthorizationInterval = 4 * time.Second

type CursorAuthorizationStatus struct {
	ID        string                  `json:"id"`
	URL       string                  `json:"url,omitempty"`
	Status    string                  `json:"status"`
	ExpiresAt time.Time               `json:"expires_at"`
	AccountID int64                   `json:"account_id,omitempty"`
	Message   string                  `json:"message,omitempty"`
	Draft     *AccountCredentialDraft `json:"draft,omitempty"`
}

type cursorAuthorization struct {
	proxyURL string
	mu       sync.Mutex
	state    CursorAuthorizationStatus
	pkce     cursor.PKCE
	nextPoll time.Time
	persist  func(context.Context, map[string]any) (int64, error)
	binding  string
	draft    func(context.Context, map[string]any) (*AccountCredentialDraft, error)
}

// Pending logins are bounded ephemeral state, not a second credential store.
// A restart requires restarting login; already authorized accounts live in DB.
type CursorAuthService struct {
	mu       sync.Mutex
	sessions map[string]*cursorAuthorization
	poll     func(context.Context, cursor.PKCE) (*cursor.OAuthTokens, error)
	now      func() time.Time
}

func NewCursorAuthService() *CursorAuthService {
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &CursorAuthService{sessions: make(map[string]*cursorAuthorization), now: time.Now, poll: func(ctx context.Context, session cursor.PKCE) (*cursor.OAuthTokens, error) {
		return cursor.PollPKCE(ctx, client, cursor.APIBaseURL, session)
	}}
}

func (service *CursorAuthService) Start(persist func(context.Context, map[string]any) (int64, error)) (CursorAuthorizationStatus, error) {
	pkce, err := cursor.NewPKCE()
	if err != nil {
		return CursorAuthorizationStatus{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	for id, session := range service.sessions {
		if service.now().After(session.state.ExpiresAt.Add(time.Minute)) {
			delete(service.sessions, id)
		}
	}
	if len(service.sessions) >= 100 {
		return CursorAuthorizationStatus{}, errors.New("Cursor authorization capacity reached; retry later")
	}
	state := CursorAuthorizationStatus{ID: pkce.UUID, URL: pkce.AuthorizationURL, Status: "waiting", ExpiresAt: service.now().Add(cursorAuthorizationLifetime)}
	service.sessions[state.ID] = &cursorAuthorization{state: state, pkce: pkce, persist: persist}
	return state, nil
}

func (service *CursorAuthService) Poll(ctx context.Context, id string, cancel bool) (CursorAuthorizationStatus, error) {
	return service.PollBound(ctx, id, "", cancel)
}

func (service *CursorAuthService) StartDraft(binding string, draft func(context.Context, map[string]any) (*AccountCredentialDraft, error), proxyURLs ...string) (CursorAuthorizationStatus, error) {
	if binding == "" || draft == nil {
		return CursorAuthorizationStatus{}, ErrPendingAuthBrowserMismatch
	}
	state, err := service.Start(nil)
	if err != nil {
		return state, err
	}
	service.mu.Lock()
	session := service.sessions[state.ID]
	session.binding, session.draft = binding, draft
	if len(proxyURLs) > 0 {
		session.proxyURL = proxyURLs[0]
	}
	service.mu.Unlock()
	return state, nil
}

func (service *CursorAuthService) PollBound(ctx context.Context, id, binding string, cancel bool) (CursorAuthorizationStatus, error) {
	service.mu.Lock()
	session := service.sessions[id]
	service.mu.Unlock()
	if session == nil {
		return CursorAuthorizationStatus{}, errors.New("Cursor authorization expired or server restarted; start again")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.binding != binding {
		return CursorAuthorizationStatus{}, ErrPendingAuthBrowserMismatch
	}
	finish := func(status, message string) {
		session.state.Status, session.state.Message = status, message
		session.pkce = cursor.PKCE{}
		session.proxyURL = ""
		session.persist = nil
		session.draft = nil
	}
	if cancel && session.state.Draft != nil {
		finish("cancelled", "Cursor authorization cancelled")
	}
	if session.state.Status != "waiting" {
		return session.state, nil
	}
	if cancel {
		finish("cancelled", "Cursor authorization cancelled")
		return session.state, nil
	}
	if !service.now().Before(session.state.ExpiresAt) {
		finish("timeout", "Cursor authorization timed out; start again")
		return session.state, nil
	}
	if service.now().Before(session.nextPoll) {
		return session.state, nil
	}
	session.nextPoll = service.now().Add(cursorAuthorizationInterval)
	pollCtx, stop := context.WithDeadline(ctx, session.state.ExpiresAt)
	defer stop()
	if session.proxyURL != "" {
		pollCtx = cursor.WithAccountProxy(pollCtx, session.proxyURL, false)
	}
	tokens, err := service.poll(pollCtx, session.pkce)
	if !service.now().Before(session.state.ExpiresAt) {
		finish("timeout", "Cursor authorization timed out; start again")
		return session.state, nil
	}
	if err != nil {
		if ctx.Err() != nil {
			finish("cancelled", "Cursor authorization cancelled")
			return session.state, nil
		}
		finish("failed", "Cursor authorization failed; start again")
		return session.state, nil
	}
	if tokens == nil {
		return session.state, nil
	}
	if !tokens.ExpiresAt.After(service.now()) {
		finish("failed", "Cursor authorization returned expired credentials")
		return session.state, nil
	}
	credentials := map[string]any{"access_token": tokens.AccessToken, "refresh_token": tokens.RefreshToken, "expires_at": strconv.FormatInt(tokens.ExpiresAt.Unix(), 10)}
	for _, field := range []string{"machine_id", "mac_machine_id"} {
		identity := make([]byte, 32)
		if _, err := rand.Read(identity); err != nil {
			finish("failed", "Cursor machine identity generation failed")
			return session.state, nil
		}
		credentials[field] = hex.EncodeToString(identity)
	}
	if session.draft != nil {
		draft, err := session.draft(pollCtx, credentials)
		if err != nil {
			finish("failed", "Cursor authorization could not save draft; start again")
			return session.state, nil
		}
		session.state.Draft = draft
		finish("ready", "Cursor authorization is ready for review and confirmation")
		return session.state, nil
	}
	accountID, err := session.persist(pollCtx, credentials)
	if err != nil {
		finish("failed", "Cursor authorization could not save account; check account settings and start again")
		return session.state, nil
	}
	session.state.AccountID = accountID
	finish("success", "Cursor account credentials saved")
	return session.state, nil
}
