//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func TestCursorUnsavedModelSync(t *testing.T) {
	for _, typ := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(typ, func(t *testing.T) {
			gateway, account, exchanges := cursorTestGateway()
			account.ID, account.Type = 0, typ
			account.Credentials["access_token"] = "preview-token"
			gateway.native = cursorProbeDriver{}
			svc := &AccountTestService{cursorGatewayService: gateway}
			catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, []string{"live"}, catalog.Models)
			require.Nil(t, account.GetUpstreamModelMetadataSnapshot())
			if typ == AccountTypeOAuth {
				require.Zero(t, exchanges.Load())
			}
			for _, failure := range []struct {
				err  error
				kind UpstreamModelSyncErrorKind
			}{
				{&cursor.HTTPError{Status: http.StatusUnauthorized}, UpstreamModelSyncErrorCredentials},
				{&cursor.HTTPError{Status: http.StatusForbidden}, UpstreamModelSyncErrorCredentials},
				{errors.New("secret upstream body"), UpstreamModelSyncErrorUpstream},
			} {
				gateway.native = cursorProbeDriver{pickerError: failure.err}
				_, err = svc.SyncUpstreamModelCatalog(context.Background(), account)
				var syncErr *UpstreamModelSyncError
				require.ErrorAs(t, err, &syncErr)
				require.Equal(t, failure.kind, syncErr.Kind)
				require.NotContains(t, syncErr.SafeMessage(), "secret")
			}
		})
	}
}

func TestCursorPreviewExpiredTokenDoesNotRefresh(t *testing.T) {
	provider := &CursorSessionProvider{}
	account := &Account{Platform: PlatformCursor, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "expired", "expires_at": time.Now().Add(-time.Hour).Format(time.RFC3339)}}
	_, err := provider.GetSessionToken(context.Background(), account, "")
	require.True(t, cursorAuthFailure(err))
}
