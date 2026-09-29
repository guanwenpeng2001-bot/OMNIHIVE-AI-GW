package cursor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// TokenExpiry reads scheduling metadata only; this does not authenticate a JWT.
func TokenExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("cursor: missing token expiry")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err != nil || json.Unmarshal(decoded, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}, errors.New("cursor: invalid token expiry")
	}
	return time.Unix(claims.Exp, 0), nil
}

type OAuthTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

var ErrReauthRequired = errors.New("cursor: invalid_grant; reauth required")

// RefreshToken posts grant_type and refresh_token as JSON to Cursor's OAuth
// endpoint; the refresh token is not a Bearer authorization credential.
// Errors never contain a request, upstream body, or token, even when the
// transport echoes its input.
func RefreshToken(ctx context.Context, client *http.Client, baseURL, refreshToken string) (OAuthTokens, error) {
	body, err := json.Marshal(struct {
		GrantType    string `json:"grant_type"`
		RefreshToken string `json:"refresh_token"`
	}{GrantType: "refresh_token", RefreshToken: refreshToken})
	if err != nil {
		return OAuthTokens{}, errors.New("cursor: invalid refresh request")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/oauth/token", bytes.NewReader(body))
	if err != nil {
		return OAuthTokens{}, errors.New("cursor: invalid refresh endpoint")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := Do(client, request, false)
	if err != nil {
		return OAuthTokens{}, errors.New("cursor: refresh transport failed")
	}
	defer func() { _ = response.Body.Close() }()
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ShouldLogout bool   `json:"shouldLogout"`
		Error        string `json:"error"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload)
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || payload.ShouldLogout || payload.Error == "invalid_grant" {
		return OAuthTokens{}, ErrReauthRequired
	}
	if response.StatusCode != http.StatusOK {
		return OAuthTokens{}, &HTTPError{Status: response.StatusCode}
	}
	// An empty token alone is inconclusive; require an explicit reauth signal.
	if decodeErr != nil || strings.TrimSpace(payload.AccessToken) == "" {
		return OAuthTokens{}, errors.New("cursor: invalid refresh response")
	}
	expiry, err := TokenExpiry(payload.AccessToken)
	if err != nil || !expiry.After(time.Now()) {
		return OAuthTokens{}, errors.New("cursor: invalid refreshed token expiry")
	}
	return OAuthTokens{AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, ExpiresAt: expiry}, nil
}
