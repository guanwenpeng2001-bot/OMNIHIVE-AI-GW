package verification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/BE-04"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/openai_compat"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type source struct {
	account                 service.Account
	credentialVersion       int64
	proxyID                 sql.NullInt64
	proxyRevision, revision string
	verifiedID              sql.NullString
}

func loadSource(ctx context.Context, q queryer, target Target) (source, error) {
	var s source
	var connectionVersion int64
	var endpoint string
	var config, extra []byte
	err := q.QueryRowContext(ctx, `SELECT pc.version, cs.version, pc.endpoint, pc.proxy_id, pc.verified_id,
		coalesce(p.updated_at::text,''), a.platform, a.type, a.credentials,
		jsonb_strip_nulls(jsonb_build_object('openai_responses_mode',a.extra->'openai_responses_mode',
			'openai_responses_supported',a.extra->'openai_responses_supported'))
		FROM provider_connections pc
		JOIN accounts a ON a.id=pc.account_id AND a.workspace_id=pc.workspace_id
		JOIN credential_secrets cs ON cs.account_id=a.id AND cs.workspace_id=a.workspace_id AND cs.revoked_at IS NULL
		LEFT JOIN proxies p ON p.id=pc.proxy_id AND p.workspace_id=pc.workspace_id
			AND p.status='active' AND p.deleted_at IS NULL AND (p.expires_at IS NULL OR p.expires_at>statement_timestamp())
		WHERE pc.workspace_id=$1 AND pc.account_id=$2 AND pc.provider=$3 AND a.platform=pc.provider
			AND pc.state='active' AND NOT pc.legacy_dirty AND a.deleted_at IS NULL AND a.status='active'
			AND a.parent_account_id IS NULL
			AND (pc.proxy_id IS NULL OR p.id IS NOT NULL)
			AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine x WHERE x.kind='accounts' AND x.resource_id=a.id)`,
		domain.DefaultWorkspaceID, target.ConnectionID, target.Provider).
		Scan(&connectionVersion, &s.credentialVersion, &endpoint, &s.proxyID, &s.verifiedID,
			&s.proxyRevision, &s.account.Platform, &s.account.Type, &config, &extra)
	if err != nil {
		return source{}, err
	}
	var fields map[string]any
	if err := json.Unmarshal(config, &fields); err != nil {
		return source{}, err
	}
	s.account.Credentials, _ = service.SplitCredentialSecrets(fields)
	service.StripCredentialReferenceKeys(s.account.Credentials)
	if endpoint != "" {
		s.account.Credentials["base_url"] = endpoint
	}
	if err := json.Unmarshal(extra, &s.account.Extra); err != nil {
		return source{}, err
	}
	s.account.ID, s.account.WorkspaceID = target.ConnectionID, domain.DefaultWorkspaceID
	// The wizard version fences managed configuration changes. Responses mode
	// also has a legacy writer in account extra and participates in the fence.
	s.revision = catalog.Digest([4]string{
		strconv.FormatInt(connectionVersion, 10), strconv.FormatInt(s.credentialVersion, 10),
		s.proxyRevision, string(extra),
	})
	return s, nil
}

func (s source) implements(m Matrix) bool {
	a := &s.account
	if !accountMatchesMode(a, m.AccountMode) || a.IsOpenAIAgentIdentity() {
		return false
	}
	identity := domain.ActionIdentity{
		Platform: a.Platform, HostedAccountType: a.Type, Protocol: a.GetCredential("api_protocol"),
	}
	action := adapterAction(m.Action, m.Protocol)
	if p, ok := domain.LookupCompatibleProvider(a.Platform); ok {
		return p.Action(a.GetCredential("account_mode"), identity, action).Status == domain.ActionImplemented
	}
	return domain.PlatformAction(identity, action).Status == domain.ActionImplemented
}

func accountMatchesMode(a *service.Account, mode string) bool {
	switch mode {
	case "api_key":
		return a.Type == domain.AccountTypeAPIKey
	case domain.AccountModePayG, domain.AccountModeCoding:
		if a.Type != domain.AccountTypeAPIKey {
			return false
		}
		if p, ok := domain.LookupCompatibleProvider(a.Platform); ok {
			m, ok := p.ResolveMode(a.GetCredential("account_mode"))
			return ok && m.Mode == mode
		}
		return a.GetAccountMode() == mode
	}
	return a.Type == mode
}

func importConnectionEvidence(ctx context.Context, tx *sql.Tx, id uuid.UUID, t Target, s source) error {
	if !s.verifiedID.Valid {
		return nil
	}
	var payload []byte
	var expiry time.Time
	err := tx.QueryRowContext(ctx, `SELECT snapshot, expires_at FROM connection_verifications
		WHERE id=$1 AND workspace_id=$2 AND account_id=$3 AND eligible AND credential_version=$4
			AND proxy_revision=$5 AND expires_at>statement_timestamp()`,
		s.verifiedID.String, domain.DefaultWorkspaceID, t.ConnectionID, s.credentialVersion, s.proxyRevision).Scan(&payload, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var previous connections.Verification
	if json.Unmarshal(payload, &previous) != nil || previous.VerifiedAt.IsZero() || previous.VerifiedAt.After(time.Now()) ||
		!previous.VerifiedAt.Before(expiry) || !slices.Contains(previous.Models, t.Model) ||
		!slices.ContainsFunc(previous.Checks, func(c connections.Check) bool { return c.Name == "discover_models" && c.State == "passed" }) {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO catalog_verification_evidence
		(id,target_id,origin,state,reason,sample_count,window_start,observed_at,expires_at,source_revision,source_ref)
		VALUES ($1,$2,'connection_verification','success','MODEL_OBSERVED',1,$3,$3,$4,$5,$6)`,
		uuid.New(), id, previous.VerifiedAt, expiry, s.revision, s.verifiedID.String)
	return err
}

type connectionProbe interface {
	ProbeConnection(context.Context, *service.Account, string) service.ConnectionProbe
}

type probeResult struct {
	state, reason, fixtureID, revision string
	samples                            int
}

func inconclusive(reason string) probeResult {
	return probeResult{state: unknown, reason: reason}
}

type ConnectionProber struct {
	db     *sql.DB
	enc    service.SecretEncryptor
	client connectionProbe
}

func NewConnectionProber(db *sql.DB, enc service.SecretEncryptor, client *service.AccountTestService) *ConnectionProber {
	return &ConnectionProber{db: db, enc: enc, client: client}
}

func (p *ConnectionProber) observe(ctx context.Context, t Target) (probeResult, error) {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return probeResult{}, err
	}
	defer tx.Rollback()
	s, err := loadSource(ctx, tx, t)
	if errors.Is(err, sql.ErrNoRows) {
		return inconclusive("CONNECTION_UNAVAILABLE"), nil
	}
	if err != nil {
		return probeResult{}, err
	}
	if !s.implements(t.Matrix) {
		return inconclusive("MATRIX_UNAVAILABLE"), nil
	}
	secret, err := credentials.Resolve(ctx, tx, p.enc, domain.DefaultWorkspaceID, t.ConnectionID, s.credentialVersion)
	if err != nil {
		return inconclusive("CREDENTIAL_UNAVAILABLE"), nil
	}
	maps.Copy(s.account.Credentials, secret)
	if s.account.IsOpenAIAgentIdentity() {
		return inconclusive("MATRIX_UNAVAILABLE"), nil
	}
	proxy, _, err := legacy.Proxy(ctx, tx, domain.DefaultWorkspaceID, proxyPolicy(s.proxyID), false)
	if err != nil {
		return probeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return probeResult{}, err
	}

	observed := p.client.ProbeConnection(ctx, &s.account, proxy)
	result := classifyObservation(observed, t.Model)
	current, err := loadSource(ctx, p.db, t)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && current.revision != s.revision) {
		return inconclusive("CONNECTION_CHANGED"), nil
	}
	if err != nil {
		return probeResult{}, err
	}
	result.revision = s.revision
	if result.state == success && t.Provider == domain.PlatformOpenAI && t.AccountMode == "api_key" &&
		t.Action == "text.chat" && t.Protocol == "openai_chat" && t.AdapterVersion == OpenAIChatAdapterVersion &&
		openai_compat.ShouldUseResponsesAPI(s.account.Extra) && checkOpenAIChatFixture() {
		result.fixtureID = OpenAIChatFixtureID
	}
	return result, nil
}

func classifyObservation(observed service.ConnectionProbe, model string) probeResult {
	switch {
	case observed.Authorization == "failed":
		return probeResult{state: failed, samples: 1, reason: "CREDENTIAL_REJECTED"}
	case observed.Network != "passed":
		return inconclusive("NETWORK_UNCONFIRMED")
	case observed.Discovery != "passed":
		return inconclusive("DISCOVERY_UNCONFIRMED")
	case !slices.Contains(observed.Models, model):
		return inconclusive("MODEL_NOT_OBSERVED")
	case observed.Authorization != "passed":
		return inconclusive("AUTHORIZATION_UNCONFIRMED")
	}
	return probeResult{state: success, samples: 1, reason: "MODEL_OBSERVED"}
}
