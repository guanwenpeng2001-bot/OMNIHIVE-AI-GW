package cursor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

type PKCE struct {
	UUID             string
	Verifier         string
	Challenge        string
	AuthorizationURL string
}

func NewPKCE() (PKCE, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return PKCE{}, errors.New("cursor: generating PKCE failed")
	}
	verifier := base64.RawURLEncoding.EncodeToString(random)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	id := uuid.NewString()
	query := url.Values{"challenge": {challenge}, "uuid": {id}, "mode": {"login"}, "redirectTarget": {"cli"}}
	return PKCE{UUID: id, Verifier: verifier, Challenge: challenge, AuthorizationURL: "https://cursor.com/loginDeepControl?" + query.Encode()}, nil
}

// PollPKCE returns nil while the authorization is pending. The verifier stays
// on the server and transport errors deliberately omit the URL query.
func PollPKCE(ctx context.Context, client *http.Client, baseURL string, session PKCE) (*OAuthTokens, error) {
	query := url.Values{"uuid": {session.UUID}, "verifier": {session.Verifier}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/auth/poll?"+query.Encode(), nil)
	if err != nil {
		return nil, errors.New("cursor: invalid authorization endpoint")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("cursor: authorization transport failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, &HTTPError{Status: response.StatusCode}
	}
	var payload struct {
		AccessToken       string `json:"accessToken"`
		RefreshToken      string `json:"refreshToken"`
		AccessTokenSnake  string `json:"access_token"`
		RefreshTokenSnake string `json:"refresh_token"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload) != nil {
		return nil, errors.New("cursor: invalid authorization response")
	}
	if payload.AccessToken == "" {
		payload.AccessToken = payload.AccessTokenSnake
	}
	if payload.RefreshToken == "" {
		payload.RefreshToken = payload.RefreshTokenSnake
	}
	if strings.TrimSpace(payload.AccessToken) == "" || strings.TrimSpace(payload.RefreshToken) == "" {
		return nil, errors.New("cursor: authorization response missing tokens")
	}
	expiry, err := TokenExpiry(payload.AccessToken)
	if err != nil {
		return nil, err
	}
	return &OAuthTokens{AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, ExpiresAt: expiry}, nil
}
