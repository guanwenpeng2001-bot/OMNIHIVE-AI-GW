package connections

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/stretchr/testify/require"
)

func TestAccountModePatchContract(t *testing.T) {
	for _, tc := range []struct {
		provider, auth, mode string
		valid                bool
	}{
		{"kilo", "api_key", "free", true}, {"kilo", "api_key", "apikey", true},
		{"opencode_go", "api_key", "go", true}, {"opencode_go", "api_key", "zen", true}, {"opencode_go", "api_key", "zen_free", true},
		{"kilo", "oauth", "free", false}, {"workbuddy", "api_key", "free", false}, {"kilo", "api_key", "", false}, {"opencode_go", "api_key", "free", false},
	} {
		c := Connection{Create: Create{Provider: tc.provider, AuthType: tc.auth}}
		p := Patch{AccountMode: &tc.mode}
		err := c.validateModePatch(p)
		if !tc.valid {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		raw, err := json.Marshal(p.Snapshot())
		require.NoError(t, err)
		var restored Patch
		require.NoError(t, json.Unmarshal(raw, &restored))
		applyPatch(&c, restored)
		resource, err := json.Marshal(c)
		require.NoError(t, err)
		require.Contains(t, string(resource), `"account_mode":"`+tc.mode+`"`)
	}
	for _, provider := range []string{"kilo", "opencode_go"} {
		mode, paid := "free", "apikey"
		if provider == "opencode_go" {
			mode, paid = "zen_free", "go"
		}
		c := Connection{Create: Create{Provider: provider, AuthType: "api_key", AccountMode: mode}, Credential: &credentials.Metadata{Version: 1, State: "unverified"}}
		var f *fault.Fault
		require.ErrorAs(t, c.validateModePatch(Patch{AccountMode: &paid}), &f)
		require.Equal(t, "CLEAR_CREDENTIAL_BEFORE_MODE_CHANGE", f.Reason)
		c.Credential.State = "revoked"
		require.NoError(t, c.validateModePatch(Patch{AccountMode: &paid}))
		applyPatch(&c, Patch{AccountMode: &paid})
		_, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "api_key", Secret: "public"})
		require.Error(t, err)
	}
}

func TestWorkBuddyCredentialMetadata(t *testing.T) {
	c := Connection{Create: Create{Provider: "workbuddy", AuthType: "oauth"}}
	meta := map[string]any{"site": "intl", "uid": "u-1", "domain": "www.workbuddy.ai", "enterprise_id": "e-1", "expires_at": float64(1800000000), "refresh_expires_at": "2030-01-01T00:00:00Z"}
	values, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "oauth", Secret: "access", RefreshToken: "refresh", Metadata: meta})
	require.NoError(t, err)
	require.Equal(t, "global", values["site"])
	require.Equal(t, "2027-01-15T08:00:00Z", values["expires_at"])
	require.Equal(t, "access", values["access_token"])
	require.Equal(t, "refresh", values["refresh_token"])
	require.Equal(t, "e-1", values["enterprise_id"])
	require.Equal(t, "intl", meta["site"])
	for _, metadata := range []map[string]any{{"api_key": "smuggled"}, {"site": "typo"}, {"uid": 12}, {"expires_at": "tomorrow"}, {"expires_at": []any{1}}, {"domain": "bad\nheader"}} {
		_, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "oauth", Secret: "access", Metadata: metadata})
		var f *fault.Fault
		require.ErrorAs(t, err, &f)
		require.Equal(t, "/metadata", f.Field)
	}
	c.AuthType = "api_key"
	for _, site := range []string{"cn", "global"} {
		values, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "api_key", Secret: "key", Metadata: map[string]any{"site": site}})
		require.NoError(t, err)
		require.Equal(t, site, values["site"])
	}
	for _, metadata := range []map[string]any{{"uid": "u"}, {"site": "intl"}, {"site": true}} {
		_, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "api_key", Secret: "key", Metadata: metadata})
		require.Error(t, err)
	}
	for _, provider := range []string{"openai", "kilo", "opencode_go"} {
		c.Provider = provider
		_, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "api_key", Secret: "", Metadata: map[string]any{"site": "cn"}})
		var f *fault.Fault
		require.ErrorAs(t, err, &f)
		require.Equal(t, "/metadata", f.Field)
	}
}

type encryptedMetadataArgument struct{ t *testing.T }

func (m encryptedMetadataArgument) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	require.NoError(m.t, err)
	var envelope struct {
		Credentials map[string]any `json:"credentials"`
	}
	require.NoError(m.t, json.Unmarshal(decoded, &envelope))
	require.Equal(m.t, "access", envelope.Credentials["access_token"])
	require.Equal(m.t, "refresh", envelope.Credentials["refresh_token"])
	require.Len(m.t, envelope.Credentials, 2)
	return true
}

func TestWorkBuddyMetadataPersistenceKeepsTokensEncrypted(t *testing.T) {
	session, mock := connectionWorkflowSession(t)
	session.store = &Store{enc: publicLaneTestEncryptor{}}
	c := Connection{ID: 3, Create: Create{Provider: "workbuddy", AuthType: "oauth"}, Credential: &credentials.Metadata{Version: 0}}
	values, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "oauth", Secret: "access", RefreshToken: "refresh", Metadata: map[string]any{"site": "global", "uid": "u-1"}})
	require.NoError(t, err)
	mock.ExpectQuery("SELECT reference FROM credential_secrets").WithArgs(2, 3, 0).WillReturnRows(sqlmock.NewRows([]string{"reference"}))
	mock.ExpectExec("SELECT set_config").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO credential_secrets").WithArgs(2, 3, 1, sqlmock.AnyArg(), encryptedMetadataArgument{t}, false, "").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT workspace_id, credentials, updated_at FROM accounts").WithArgs(3).WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "credentials", "updated_at"}).AddRow(2, []byte(`{}`), time.Now()))
	mock.ExpectExec("UPDATE accounts SET credentials=\\$2").WithArgs(3, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs("account_changed", 3).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE accounts SET credentials=\\(coalesce").WithArgs(2, 3, `{"site":"global","uid":"u-1"}`).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, session.changeSecret(c, values, false))
	require.NoError(t, mock.ExpectationsWereMet())
}

func accountModeRows(provider, mode string, version int64, revoked bool) *sqlmock.Rows {
	columns := []string{"id", "workspace", "provider", "auth", "name", "endpoint", "proxy", "models", "pools", "concurrency", "state", "version", "credential_version", "revoked", "reference", "snapshot", "fingerprint", "last_four", "fetched", "next_refresh", "quota", "used", "headers", "account_mode"}
	var revokedAt any
	if revoked {
		revokedAt = time.Now()
	}
	return sqlmock.NewRows(columns).AddRow(3, 2, provider, "api_key", "lane", "", "", "{first-model}", "{}", 3, "disabled", version, 1, revokedAt, "ref", nil, nil, nil, nil, nil, nil, nil, nil, mode)
}

func TestAccountModePatchPersistsAndInvalidatesVerification(t *testing.T) {
	for _, lane := range []struct{ provider, old, new string }{{"kilo", "free", "apikey"}, {"opencode_go", "zen_free", "go"}, {"opencode_go", "zen_free", "zen"}} {
		t.Run(lane.provider+lane.new, func(t *testing.T) {
			session, mock := connectionWorkflowSession(t)
			mock.ExpectQuery("SELECT id FROM accounts").WithArgs(2, 3).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
			mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(accountModeRows(lane.provider, lane.old, 4, true))
			mock.ExpectExec("SELECT set_config").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("UPDATE accounts SET credentials=").WithArgs(2, 3, lane.new).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs("account_changed", 3).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("UPDATE provider_connections SET name").WithArgs(2, 3, "lane", "", "", `{"first-model"}`, "{}", 3, "disabled", true).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO connection_events").WithArgs(2, 3, 1, "connection.edited", 5).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(accountModeRows(lane.provider, lane.new, 5, true))
			result, err := session.Patch(Patch{AccountMode: &lane.new})
			require.NoError(t, err)
			connection, ok := result.(Connection)
			require.True(t, ok)
			require.Equal(t, lane.new, connection.AccountMode)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
	session, mock := connectionWorkflowSession(t)
	mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(accountModeRows("kilo", "", 4, false))
	c, err := session.get(3, false)
	require.NoError(t, err)
	require.Equal(t, "free", c.AccountMode)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPreviewRejectsUnverifiedModeChange(t *testing.T) {
	session, _ := connectionWorkflowSession(t)
	c := Connection{Create: Create{Provider: "opencode_go", AuthType: "api_key", AccountMode: "go"}, Credential: &credentials.Metadata{Version: 1, State: "revoked"}, Verification: &Verification{ID: "old-mode"}}
	mode := "zen"
	var f *fault.Fault
	require.ErrorAs(t, session.applicable(c, Patch{AccountMode: &mode}), &f)
	require.Equal(t, "VERIFICATION_REQUIRED", f.Reason)
}
