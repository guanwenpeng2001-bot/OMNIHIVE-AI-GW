package cursor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func syntheticOAuthJWT() string {
	return "synthetic." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(time.Hour).Unix()))) + ".signature"
}

func TestCursorRefreshVerifiedContract(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		t.Run(fmt.Sprintf("rotation=%v", rotate), func(t *testing.T) {
			access := syntheticOAuthJWT()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "/oauth/token", r.URL.Path)
				require.Equal(t, "Bearer synthetic-refresh", r.Header.Get("Authorization"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.JSONEq(t, `{}`, string(body))
				payload := map[string]any{"access_token": access, "id_token": "unused", "shouldLogout": false}
				if rotate {
					payload["refresh_token"] = "rotated-synthetic-refresh"
				}
				require.NoError(t, json.NewEncoder(w).Encode(payload))
			}))
			defer server.Close()
			tokens, err := RefreshToken(context.Background(), server.Client(), server.URL, "synthetic-refresh")
			require.NoError(t, err)
			require.Equal(t, access, tokens.AccessToken)
			expiry, err := TokenExpiry(access)
			require.NoError(t, err)
			require.Equal(t, expiry, tokens.ExpiresAt)
			if rotate {
				require.Equal(t, "rotated-synthetic-refresh", tokens.RefreshToken)
			} else {
				require.Empty(t, tokens.RefreshToken)
			}
		})
	}
}

func TestCursorRefreshFailureClassificationAndRedaction(t *testing.T) {
	for _, test := range []struct {
		status    int
		body      string
		permanent bool
	}{
		{401, `secret echoed by upstream`, true}, {400, `{"error":"invalid_grant","detail":"secret"}`, true},
		{200, `{"shouldLogout":true}`, true}, {503, `secret`, false}, {200, `{"access_token":"secret"}`, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(test.status)
			_, _ = io.WriteString(w, test.body)
		}))
		_, err := RefreshToken(context.Background(), server.Client(), server.URL, "secret")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
		if test.permanent {
			require.ErrorIs(t, err, ErrReauthRequired)
		} else {
			require.NotErrorIs(t, err, ErrReauthRequired)
		}
		server.Close()
	}
}

func TestCursorPKCEGenerationAndPollingContract(t *testing.T) {
	session, err := NewPKCE()
	require.NoError(t, err)
	_, err = uuid.Parse(session.UUID)
	require.NoError(t, err)
	require.Len(t, session.Verifier, 43)
	digest := sha256.Sum256([]byte(session.Verifier))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(digest[:]), session.Challenge)
	authURL, err := url.Parse(session.AuthorizationURL)
	require.NoError(t, err)
	require.Equal(t, "cursor.com", authURL.Host)
	require.Equal(t, "/loginDeepControl", authURL.Path)
	require.Equal(t, session.Challenge, authURL.Query().Get("challenge"))
	require.NotContains(t, session.AuthorizationURL, session.Verifier)
	for _, shape := range []string{"pending", "camel", "snake"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "GET", r.Method)
			require.Equal(t, "/auth/poll", r.URL.Path)
			require.Equal(t, session.UUID, r.URL.Query().Get("uuid"))
			require.Equal(t, session.Verifier, r.URL.Query().Get("verifier"))
			if shape == "pending" {
				w.WriteHeader(404)
				return
			}
			accessKey, refreshKey := "accessToken", "refreshToken"
			if shape == "snake" {
				accessKey, refreshKey = "access_token", "refresh_token"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{accessKey: syntheticOAuthJWT(), refreshKey: "synthetic-refresh"})
		}))
		tokens, err := PollPKCE(context.Background(), server.Client(), server.URL, session)
		require.NoError(t, err)
		if shape == "pending" {
			require.Nil(t, tokens)
		} else {
			require.Equal(t, "synthetic-refresh", tokens.RefreshToken)
		}
		server.Close()
	}
}
