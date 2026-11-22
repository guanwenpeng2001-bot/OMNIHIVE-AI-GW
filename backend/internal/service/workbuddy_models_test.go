//go:build unit

package service

import (
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
