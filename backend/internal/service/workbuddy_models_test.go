//go:build unit

package service

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkBuddyCLIModelSelectionAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name, agents string
		want         []string
	}{
		{"missing CLI", `[{"name":"desktop","models":["local-model"]}]`, []string{}},
		{"no agents", `[]`, []string{}},
		{"empty CLI", `[{"name":"cli","models":[]}]`, []string{"custom:team|exact"}},
		{"CLI and enterprise", `[{"name":"cli","models":["cloud","cloud"]}]`, []string{"cloud", "custom:team|exact"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"code":0,"data":{"models":[{"id":"local-model","modelType":"custom"},{"id":"cloud"},{"id":"custom:team|exact","modelType":"enterprise"}],"agents":` + tc.agents + `}}`)
			ids, _, err := extractWorkBuddyModelCatalog(raw, "cn")
			require.NoError(t, err)
			require.Equal(t, tc.want, ids)
			for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
				a := wbAccount("cn", "", kind)
				if kind == AccountTypeAPIKey {
					a.Credentials["api_key"] = "ordinary-key"
				}
				body := workBuddyConfigFallback(raw, a)
				got, _, err := extractWorkBuddyModelCatalog(body, "cn")
				require.NoError(t, err)
				if len(tc.want) == 0 && kind == AccountTypeAPIKey {
					require.NotEmpty(t, got)
					require.NotContains(t, got, "local-model")
				} else {
					require.Equal(t, tc.want, got)
				}
			}
		})
	}
	for _, raw := range []string{`{"code":401,"data":{}}`, `{"code":403,"data":{}}`, `{"data":{}}`} {
		require.Equal(t, raw, string(workBuddyConfigFallback([]byte(raw), wbAccount("cn", "", AccountTypeAPIKey))))
	}
	for _, key := range []string{"ck_private", "pt_private"} {
		a := wbAccount("cn", "", AccountTypeAPIKey)
		a.Credentials["api_key"] = key
		raw := []byte(`{"code":0,"data":{"models":[],"agents":[]}}`)
		require.Equal(t, raw, workBuddyConfigFallback(raw, a))
	}
}

func TestW05WorkBuddySparseFactsAndBusinessErrors(t *testing.T) {
	for _, tc := range []struct {
		fields            string
		vision, reasoning *bool
	}{
		{``, nil, nil}, {`,"supportsImages":null,"supportsReasoning":null`, nil, nil},
		{`,"supportsImages":false,"supportsReasoning":false`, w05Bool(false), w05Bool(false)},
		{`,"supportsImages":true,"supportsReasoning":true`, w05Bool(true), w05Bool(true)},
		{`,"supportsImages":"invalid","supportsReasoning":true`, nil, w05Bool(true)},
	} {
		raw := []byte(`{"code":0,"data":{"models":[{"id":"one"` + tc.fields + `}],"agents":[{"name":"cli","models":["one","cli-only"]}]}}`)
		_, meta, err := extractWorkBuddyModelCatalog(raw, "cn")
		require.NoError(t, err)
		require.Equal(t, tc.vision, meta["one"].SupportsVision)
		require.Equal(t, tc.reasoning, meta["one"].Reasoning)
		require.Nil(t, meta["cli-only"].SupportsVision)
		require.Nil(t, meta["cli-only"].Reasoning)
		require.Empty(t, meta["cli-only"].InputModalities)
	}
	for _, code := range []int{401, 403, 429, 999} {
		_, _, err := extractWorkBuddyModelCatalog([]byte(fmt.Sprintf(`{"code":%d}`, code)), "cn")
		var failure *UpstreamModelSyncError
		require.ErrorAs(t, err, &failure)
		result := connectionProbeCatalogFailure(err)
		if code == 401 || code == 403 {
			require.Equal(t, UpstreamModelSyncErrorCredentials, failure.Kind)
			require.Equal(t, "AUTH_REJECTED", result.AuthorizationReason)
		}
		if code == 429 {
			require.Equal(t, "UPSTREAM_RATE_LIMITED", result.DiscoveryReason)
		}
		if code == 999 {
			require.NotEqual(t, "AUTH_REJECTED", result.AuthorizationReason)
		}
	}
}

func w05Bool(v bool) *bool { return &v }
