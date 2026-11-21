package connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

func connectionWorkflowSession(t *testing.T) (*Session, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	return &Session{tx: tx, ctx: ctx, Command: Command{Actor: 1, Workspace: 2, ID: 3, Version: 4},
		access: identity.Access{Actions: []string{"connections.read", "connections.edit"}}}, mock
}

func connectionWorkflowRows(state, name string, version int64, verification []byte) *sqlmock.Rows {
	columns := []string{
		"account_id", "workspace_id", "provider", "auth_type", "name", "endpoint", "proxy_id",
		"model_ids", "pool_ids", "concurrency", "state", "version", "credential_version", "revoked_at",
		"reference", "snapshot", "fingerprint", "last_four", "fetched", "next_refresh", "quota_status",
		"used", "headers", "account_mode",
	}
	return sqlmock.NewRows(columns).AddRow(
		int64(3), int64(2), "openai", "api_key", name, "https://example.test", "",
		"{first-model}", "{}", int64(3), state, version, int64(1), nil, "reference",
		verification, nil, nil, nil, nil, nil, nil, nil, "",
	)
}

func TestPatchPreservesConnectionState(t *testing.T) {
	for _, state := range []string{"active", "disabled", "draft"} {
		t.Run(state, func(t *testing.T) {
			session, mock := connectionWorkflowSession(t)
			mock.ExpectQuery("SELECT id FROM accounts").WithArgs(2, 3).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
			mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(connectionWorkflowRows(state, "before", 4, nil))
			name := "after"
			mock.ExpectExec("UPDATE provider_connections SET name").
				WithArgs(2, 3, name, "https://example.test", "", `{"first-model"}`, "{}", 3, state, false).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO connection_events").
				WithArgs(2, 3, 1, "connection.edited", 5).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(connectionWorkflowRows(state, name, 5, nil))
			value, err := session.Patch(Patch{Name: &name})
			require.NoError(t, err)
			conn, ok := value.(Connection)
			require.True(t, ok)
			require.Equal(t, name, conn.Name)
			require.Equal(t, state, conn.State)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPreviewPreservesActiveConnectionAndEvaluatesCandidate(t *testing.T) {
	session, mock := connectionWorkflowSession(t)
	verification, err := json.Marshal(Verification{
		ID: "verified-probe", Models: []string{"first-model", "candidate"}, Expires: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	rows := func() *sqlmock.Rows { return connectionWorkflowRows("active", "existing", 4, verification) }
	mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(rows())
	mock.ExpectQuery("SELECT EXISTS\\(SELECT 1 FROM connection_verifications").
		WithArgs("verified-probe", 2, 3, 1, "").WillReturnRows(sqlmock.NewRows([]string{"eligible"}).AddRow(true))
	mock.ExpectExec("INSERT INTO connection_previews").
		WithArgs(sqlmock.AnyArg(), 2, 1, 3, `{"model_ids":["candidate"]}`, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(rows())

	result, err := session.Preview(nil, Patch{Models: []string{"candidate"}})
	require.NoError(t, err)
	preview, ok := result.(Preview)
	require.True(t, ok)
	require.Equal(t, "applicable", preview.Items[0].State)
	live, err := session.Get()
	require.NoError(t, err)
	liveConn, ok := live.(Connection)
	require.True(t, ok)
	require.Equal(t, "active", liveConn.State)
	require.Equal(t, []string{"first-model"}, liveConn.Models)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPreviewDistinguishesUnavailableEvidenceFromMissingProbe(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows *sqlmock.Rows
		want string
	}{
		{"missing probe", sqlmock.NewRows([]string{"snapshot", "eligible"}), "VERIFICATION_REQUIRED"},
		{"unreachable probe", sqlmock.NewRows([]string{"snapshot", "eligible"}).AddRow([]byte(`{"checks":[{"name":"network","state":"unknown","reason":"EVIDENCE_UNAVAILABLE"}]}`), false), "EVIDENCE_UNAVAILABLE"},
		{"failed probe", sqlmock.NewRows([]string{"snapshot", "eligible"}).AddRow([]byte(`{"checks":[{"name":"authorization","state":"failed","reason":"PROBE_FAILED"}]}`), false), "PROBE_FAILED"},
		{"previously eligible probe", sqlmock.NewRows([]string{"snapshot", "eligible"}).AddRow([]byte(`{}`), true), "VERIFICATION_REQUIRED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, mock := connectionWorkflowSession(t)
			mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(connectionWorkflowRows("active", "existing", 4, nil))
			mock.ExpectQuery("SELECT snapshot,eligible FROM connection_verifications").WithArgs(2, 3, 4, 1, "").WillReturnRows(tc.rows)
			mock.ExpectExec("INSERT INTO connection_previews").WillReturnResult(sqlmock.NewResult(0, 1))

			result, err := session.Preview(nil, Patch{Models: []string{"candidate"}})
			require.NoError(t, err)
			preview, ok := result.(Preview)
			require.True(t, ok)
			require.Equal(t, []string{tc.want}, preview.Blocked)
			require.Equal(t, "blocked", preview.Items[0].State)
			require.NotEmpty(t, preview.Items[0].Message)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

type publicLaneTestEncryptor struct{}

func (publicLaneTestEncryptor) EncryptionKeyConfigured() bool { return true }
func (publicLaneTestEncryptor) Encrypt(s string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}
func (publicLaneTestEncryptor) Decrypt(s string) (string, error) {
	b, e := base64.StdEncoding.DecodeString(s)
	return string(b), e
}

func TestPublicLaneConnectionCreatesCredentialVersion(t *testing.T) {
	for _, lane := range []struct{ provider, mode string }{{"kilo", ""}, {"opencode_go", "zen_free"}} {
		t.Run(lane.provider, func(t *testing.T) {
			session, mock := connectionWorkflowSession(t)
			session.Version = 0
			session.store = &Store{enc: publicLaneTestEncryptor{}}
			mock.ExpectQuery("INSERT INTO accounts").WithArgs(2, "free", lane.provider, "apikey").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
			mock.ExpectExec("INSERT INTO provider_connections").WithArgs(3, 2, "free", lane.provider, "api_key", "", "").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT reference FROM credential_secrets").WithArgs(2, 3, 0).WillReturnRows(sqlmock.NewRows([]string{"reference"}))
			mock.ExpectExec("SELECT set_config").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO credential_secrets").WithArgs(2, 3, 1, sqlmock.AnyArg(), sqlmock.AnyArg(), false, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT workspace_id, credentials, updated_at FROM accounts").WithArgs(3).WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "credentials", "updated_at"}).AddRow(2, []byte(`{}`), time.Now()))
			mock.ExpectExec("UPDATE accounts SET credentials=\\$2").WithArgs(3, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs("account_changed", 3).WillReturnResult(sqlmock.NewResult(0, 1))
			mode := lane.mode
			if mode == "" {
				mode = "free"
			}
			mock.ExpectExec("UPDATE accounts SET credentials=coalesce").WithArgs(2, 3, mode).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO connection_events").WithArgs(2, 3, 1, "connection.created", 1).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT c.account_id").WithArgs(2, 3).WillReturnRows(connectionWorkflowRows("draft", "free", 1, nil))
			_, err := session.Create(Create{Provider: lane.provider, AccountMode: lane.mode, AuthType: "api_key", SchemaVersion: SchemaVersion, Name: "free"})
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
func TestPublicLaneCredentialReplacement(t *testing.T) {
	for _, c := range []Connection{{Provider: "kilo", AuthType: "api_key"}, {Provider: "opencode_go", AuthType: "api_key", AccountMode: "zen_free"}} {
		for _, key := range []string{"", "public"} {
			values, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "api_key", Secret: key})
			require.NoError(t, err)
			want := ""
			if c.Provider == "opencode_go" {
				want = "public"
			}
			require.Equal(t, want, values["api_key"])
		}
		_, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "api_key", Secret: "private"})
		require.Error(t, err)
	}
	c := Connection{Provider: "kilo", AuthType: "api_key", AccountMode: "apikey"}
	_, err := c.credentialPayload(credentials.Change{Operation: "set", Type: "api_key", Secret: "public"})
	require.Error(t, err)
}
