//go:build unit

package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCursorBuiltinMetadataSyncAndUnknowns(t *testing.T) {
	for _, id := range []string{"claude-sonnet-5", "default", "unverified-model"} {
		t.Run(id, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": id}}})
			}))
			defer server.Close()
			gateway, account, _ := cursorTestGateway()
			gateway.relayClient = server.Client()
			account.Type = AccountTypeUpstream
			account.Credentials["base_url"] = server.URL
			svc := &AccountTestService{cursorGatewayService: gateway, accountRepo: &upstreamModelMetadataRepoStub{}}
			catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
			require.NoError(t, err)
			if id == "claude-sonnet-5" {
				require.Empty(t, catalog.Warnings)
				require.Equal(t, int64(1000000), catalog.Metadata[id].ContextWindow)
				require.NotEmpty(t, catalog.Metadata[id].SupportedReasoningLevels)
				require.Equal(t, "cursor-builtin", account.GetUpstreamModelMetadataSnapshot().Source)
			} else {
				require.NotEmpty(t, catalog.Warnings)
				require.Nil(t, account.GetUpstreamModelMetadataSnapshot())
			}
		})
	}
}
