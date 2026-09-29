package cursor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const APIBaseURL = "https://api2.cursor.sh"

type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type HTTPError struct{ Status int }

func (failure *HTTPError) Error() string {
	return fmt.Sprintf("cursor upstream HTTP %d", failure.Status)
}

// ExchangeKey never returns response bodies or the supplied key in errors.
func ExchangeKey(ctx context.Context, client *http.Client, baseURL, apiKey string) (Session, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/auth/exchange_user_api_key", strings.NewReader("{}"))
	if err != nil {
		return Session{}, fmt.Errorf("cursor: invalid exchange endpoint")
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := Do(client, request, false)
	if err != nil {
		return Session{}, fmt.Errorf("cursor: exchange transport failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Session{}, &HTTPError{Status: response.StatusCode}
	}
	var payload struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil || payload.AccessToken == "" {
		return Session{}, fmt.Errorf("cursor: invalid exchange response")
	}
	now := time.Now()
	expiry := now.Add(time.Hour)
	if payload.ExpiresIn > 0 && now.Add(time.Duration(payload.ExpiresIn)*time.Second).Before(expiry) {
		expiry = now.Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	parts := strings.Split(payload.AccessToken, ".")
	if len(parts) == 3 {
		decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
		var claims struct {
			Exp int64 `json:"exp"`
		}
		if err == nil && json.Unmarshal(decoded, &claims) == nil && claims.Exp > 0 && time.Unix(claims.Exp, 0).Before(expiry) {
			expiry = time.Unix(claims.Exp, 0)
		}
	}
	if !expiry.After(now) {
		return Session{}, fmt.Errorf("cursor: exchanged session already expired")
	}
	// Keep short-lived sessions usable while retaining a conservative refresh skew.
	skew := min(5*time.Minute, expiry.Sub(now)/10)
	return Session{Token: payload.AccessToken, ExpiresAt: expiry.Add(-skew)}, nil
}
