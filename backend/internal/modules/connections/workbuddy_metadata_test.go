package connections

import (
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/stretchr/testify/require"
)

func TestW05WorkBuddyMetadataMergeAndProjection(t *testing.T) {
	c := Connection{Create: Create{Provider: "workbuddy", AuthType: "oauth"}, Metadata: map[string]any{"site": "global", "uid": "old-user", "domain": "old.domain", "enterprise_id": "team", "expires_at": "2030-01-01T00:00:00Z", "access_token": "never-return", "api_key": "never-return"}}
	values, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "oauth", Secret: "rotated-token", Metadata: map[string]any{"uid": "new-user", "domain": ""}})
	require.NoError(t, err)
	require.Equal(t, "global", values["site"])
	require.Equal(t, "team", values["enterprise_id"])
	require.Equal(t, "", values["domain"])
	require.Equal(t, "new-user", values["uid"])
	require.Equal(t, "rotated-token", values["access_token"])
	require.NotContains(t, values, "api_key")
	values, err = c.credentialPayload(credentials.Change{Operation: "keep", Metadata: map[string]any{"uid": "metadata-only", "enterprise_id": nil}})
	require.NoError(t, err)
	require.Equal(t, "metadata-only", values["uid"])
	require.Equal(t, "", values["enterprise_id"])
	require.NotContains(t, values, "access_token")
	_, err = c.credentialPayload(credentials.Change{Operation: "keep", Secret: "not-allowed", Metadata: map[string]any{"uid": "u"}})
	require.Error(t, err)
	_, err = c.credentialPayload(credentials.Change{Operation: "keep", Metadata: map[string]any{"refresh_token": "bad"}})
	require.Error(t, err)
	projected := c.Project(identity.Access{Actions: []string{"connections.read"}})
	raw, err := json.Marshal(projected)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"metadata"`)
	require.NotContains(t, string(raw), "never-return")
	require.NotContains(t, string(raw), "access_token")
	var patch Patch
	require.NoError(t, json.Unmarshal([]byte(`{"metadata":{"site":"cn","uid":""}}`), &patch))
	require.Contains(t, patch.Snapshot(), "metadata")
	require.Equal(t, "old-user", c.Metadata["uid"], "merge must not mutate the stored snapshot")
}
func TestW05WorkBuddyMetadataPersistenceWithoutSecretRotation(t *testing.T) {
	session, mock := connectionWorkflowSession(t)
	c := Connection{ID: 3, Create: Create{Provider: "workbuddy", AuthType: "oauth"}, Metadata: map[string]any{"uid": "u", "site": "global"}}
	values, err := c.credentialPayload(credentials.Change{Operation: "keep", Metadata: map[string]any{"domain": "updated.domain"}})
	require.NoError(t, err)
	mock.ExpectExec("SELECT set_config").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE accounts SET credentials=").WithArgs(2, 3, `{"domain":"updated.domain","site":"global","uid":"u"}`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs("account_changed", 3).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, session.saveWorkBuddyMetadata(c, values))
	require.NoError(t, mock.ExpectationsWereMet())
}

func w05WorkBuddyRows(version int64, metadata string) *sqlmock.Rows {
	columns := []string{"account_id", "workspace_id", "provider", "auth_type", "name", "endpoint", "proxy_id", "model_ids", "pool_ids", "concurrency", "state", "version", "credential_version", "revoked_at", "reference", "snapshot", "fingerprint", "last_four", "fetched", "next_refresh", "quota_status", "used", "headers", "account_mode", "metadata"}
	return sqlmock.NewRows(columns).AddRow(3, 2, "workbuddy", "oauth", "WorkBuddy", "", "", "{}", "{}", 3, "disabled", version, 1, nil, "reference", nil, nil, nil, nil, nil, nil, nil, nil, "", []byte(metadata))
}
func TestW05WorkBuddyMetadataOnlyPatch(t *testing.T) {
	session, mock := connectionWorkflowSession(t)
	mock.ExpectQuery("SELECT id FROM accounts").WithArgs(2, 3).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(w05WorkBuddyRows(4, `{"site":"global","uid":"u"}`))
	mock.ExpectExec("SELECT set_config").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE accounts SET credentials=").WithArgs(2, 3, `{"domain":"new.domain","site":"global","uid":"u"}`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs("account_changed", 3).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE provider_connections SET name").WithArgs(2, 3, "WorkBuddy", "", "", "{}", "{}", 3, "disabled", true).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO connection_events").WithArgs(2, 3, 1, "connection.edited", 5).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(w05WorkBuddyRows(5, `{"domain":"new.domain","site":"global","uid":"u"}`))
	result, err := session.Patch(Patch{Metadata: map[string]any{"domain": "new.domain"}})
	require.NoError(t, err)
	conn, ok := result.(Connection)
	require.True(t, ok)
	require.Equal(t, "new.domain", conn.Metadata["domain"])
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestW05WorkBuddyKeepMetadataDoesNotRotateTokens(t *testing.T) {
	session, mock := connectionWorkflowSession(t)
	session.access.Actions = append(session.access.Actions, "credentials.write")
	mock.ExpectQuery("SELECT id FROM accounts").WithArgs(2, 3).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(w05WorkBuddyRows(4, `{"site":"global","uid":"u"}`))
	mock.ExpectExec("SELECT set_config").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE accounts SET credentials=").WithArgs(2, 3, `{"site":"global","uid":"new-u"}`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs("account_changed", 3).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE provider_connections SET name").WithArgs(2, 3, "WorkBuddy", "", "", "{}", "{}", 3, "disabled", true).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO connection_events").WithArgs(2, 3, 1, "connection.edited", 5).WillReturnResult(sqlmock.NewResult(0, 1))
	result, err := session.Credential(credentials.Change{Operation: "keep", Metadata: map[string]any{"uid": "new-u"}})
	require.NoError(t, err)
	resMap, ok := result.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "5", resMap["version"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestW05WorkBuddyMetadataRejectsNestedSecretProjection(t *testing.T) {
	safe := safeWorkBuddyMetadata("workbuddy", map[string]any{"uid": map[string]any{"access_token": "secret"}, "domain": []any{"secret"}, "site": "global", "expires_at": int64(1800000000)})
	require.NotContains(t, safe, "uid")
	require.NotContains(t, safe, "domain")
	require.Equal(t, "global", safe["site"])
	raw, err := json.Marshal(safe)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret")
}
