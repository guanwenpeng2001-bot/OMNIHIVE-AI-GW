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
				// Builtin context and reasoning evidence do not make the capability
				// profile complete: input modalities are partial and output limits
				// remain unknown, so the unconfigured registry must still warn.
				require.Equal(t, "partial", catalog.Metadata[id].InputModalitiesStatus)
				require.Contains(t, catalog.Metadata[id].MissingFields, "input_modalities")
				require.Contains(t, catalog.Metadata[id].MissingFields, "task_type")
				require.Zero(t, catalog.Metadata[id].MaxOutputTokens)
				require.Len(t, catalog.Warnings, 2, "incomplete builtin metadata and unavailable registry both need diagnostics")
				require.Equal(t, UpstreamModelMetadataIncompleteCode, catalog.Warnings[0].Code)
				require.Equal(t, "model_metadata_registry_unavailable", catalog.Warnings[1].Code)
				require.Equal(t, "model metadata registry is not configured", catalog.Warnings[1].Message)
				require.Equal(t, int64(1000000), catalog.Metadata[id].ContextWindow)
				require.NotEmpty(t, catalog.Metadata[id].SupportedReasoningLevels)
				require.Equal(t, "cursor-builtin", catalog.Metadata[id].FieldEvidence["context_window"].Source)
				require.Equal(t, "cursor-builtin", catalog.Metadata[id].FieldEvidence["supported_reasoning_levels"].Source)
				// Discovery projects evidence; the reviewed apply flow owns persistence.
				require.Nil(t, account.GetUpstreamModelMetadataSnapshot())
			} else {
				require.NotEmpty(t, catalog.Warnings)
				require.Nil(t, account.GetUpstreamModelMetadataSnapshot())
			}
		})
	}
}
