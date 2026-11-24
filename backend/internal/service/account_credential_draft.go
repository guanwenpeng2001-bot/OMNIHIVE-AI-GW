package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/pendingauthsession"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/urlvalidator"
)

const accountCredentialDraftPurpose = "account-credential-draft"

// AccountCredentialDraft is the entire browser-visible authorization result.
// Credentials and provider responses never belong in this projection.
type AccountCredentialDraft struct {
	Reference string    `json:"credential_draft"`
	Platform  string    `json:"platform"`
	Type      string    `json:"type"`
	ExpiresAt time.Time `json:"expires_at"`
	Revision  string    `json:"revision"`
}

type accountCredentialDraftState struct {
	Egress                     *OAuthEgressSelection `json:"egress,omitempty"`
	Platform                   string                `json:"platform"`
	Type                       string                `json:"type"`
	Credentials                map[string]any        `json:"credentials,omitempty"`
	Revision                   string                `json:"revision"`
	Submission                 string                `json:"submission,omitempty"`
	AccountID                  int64                 `json:"account_id,omitempty"`
	Cancelled                  bool                  `json:"cancelled,omitempty"`
	ReviewDigest               string                `json:"review_digest,omitempty"`
	ReviewedCredentialRevision string                `json:"reviewed_credential_revision,omitempty"`
}

var (
	ErrAccountCredentialDraftConflict      = infraerrors.Conflict("ACCOUNT_CREDENTIAL_DRAFT_CONFLICT", "Credential draft changed; preview and confirm again")
	ErrAccountCredentialDraftReviewRequired = infraerrors.Conflict("ACCOUNT_CREDENTIAL_DRAFT_CONFLICT", "Credential draft has no reviewed account revision; review again before confirming")
)

// ApplyAccountCredentialDraftOptions is the final-review configuration allowlist.
// Authentication, machine and owner identifiers can only come from the opaque
// draft. Model discovery deliberately uses the narrower model-only overlay.
func ApplyAccountCredentialDraftOptions(account *Account, options map[string]any) error {
	validated := make(map[string]any, len(options))
	invalid := func(field string) error {
		return infraerrors.BadRequest("INVALID_CREDENTIAL_DRAFT_OPTION", fmt.Sprintf("Invalid or unsupported credential draft option: %s", field)).WithMetadata(map[string]string{"field": field})
	}
	for key, value := range options {
		switch key {
		case "model_mapping", "compact_model_mapping":
			switch mapping := value.(type) {
			case map[string]string:
			case map[string]any:
				for _, target := range mapping {
					if _, ok := target.(string); !ok {
						return invalid(key)
					}
				}
			default:
				return invalid(key)
			}
		case "plan_type", "client_version", "antigravity_project_id":
			if _, ok := value.(string); !ok {
				return invalid(key)
			}
		case "base_url":
			endpoint, ok := value.(string)
			if !ok {
				return invalid(key)
			}
			if endpoint != "" {
				if _, err := urlvalidator.ValidateURLFormat(endpoint, true); err != nil {
					return invalid(key)
				}
			}
		case "account_scheduling_threshold":
			if _, ok := parseAccountSchedulingThresholdValue(value); !ok {
				return invalid(key)
			}
		case "intercept_warmup_requests", "temp_unschedulable_enabled", "header_override_enabled":
			if _, ok := value.(bool); !ok {
				return invalid(key)
			}
		case "temp_unschedulable_rules":
			rules, ok := value.([]any)
			if !ok {
				return invalid(key)
			}
			candidate := &Account{Credentials: map[string]any{key: rules}}
			if len(candidate.GetTempUnschedulableRules()) != len(rules) {
				return invalid(key)
			}
		case "header_overrides":
			// The shared normalizer below validates names, values and blocked headers.
		default:
			return invalid(key)
		}
		validated[key] = value
	}
	if err := NormalizeHeaderOverrideCredentials(validated); err != nil {
		return err
	}
	if account.Credentials == nil {
		account.Credentials = map[string]any{}
	}
	for key, value := range validated {
		account.Credentials[key] = value
	}
	return nil
}

func (s *AuthPendingIdentityService) IssueAccountCredentialDraft(ctx context.Context, binding, platform, accountType string, credentials map[string]any, selections ...*OAuthEgressSelection) (*AccountCredentialDraft, error) {
	if strings.TrimSpace(platform) == "" || (accountType != AccountTypeOAuth && accountType != AccountTypeSetupToken) || len(credentials) == 0 {
		return nil, ErrPendingAuthCodeInvalid
	}
	revision, err := randomOpaqueToken(24)
	if err != nil {
		return nil, err
	}
	var selection *OAuthEgressSelection
	if len(selections) > 0 && selections[0] != nil {
		copy := *selections[0]
		selection = &copy
	}
	state := accountCredentialDraftState{Egress: selection, Platform: platform, Type: accountType, Credentials: SanitizeStoredCredentials(platform, credentials), Revision: revision}
	data, err := accountCredentialDraftData(state)
	if err != nil {
		return nil, err
	}
	reference, err := s.IssueIdentityTicket(ctx, accountCredentialDraftPurpose, binding, data, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	return s.PreviewAccountCredentialDraft(ctx, reference, binding)
}

func accountCredentialDraftData(state accountCredentialDraftState) (map[string]any, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	// One encoded payload avoids lossy float64 round trips for IDs and revisions.
	return map[string]any{"credential_draft_state": string(encoded)}, nil
}

func readAccountCredentialDraft(session *dbent.PendingAuthSession, binding string) (*accountCredentialDraftState, error) {
	if session.ProviderKey != identityTicketProviderKey || session.ProviderSubject != accountCredentialDraftPurpose {
		return nil, ErrPendingAuthSessionNotFound
	}
	if strings.TrimSpace(binding) == "" || subtle.ConstantTimeCompare([]byte(session.BrowserSessionKey), []byte(binding)) != 1 {
		return nil, ErrPendingAuthBrowserMismatch
	}
	if !time.Now().Before(session.ExpiresAt) {
		return nil, ErrPendingAuthSessionExpired
	}
	encoded, ok := session.LocalFlowState["credential_draft_state"].(string)
	if !ok {
		return nil, ErrPendingAuthSessionNotFound
	}
	var state accountCredentialDraftState
	if err := json.Unmarshal([]byte(encoded), &state); err != nil {
		return nil, ErrPendingAuthSessionNotFound
	}
	return &state, nil
}

func (s *AuthPendingIdentityService) accountCredentialDraft(ctx context.Context, reference, binding string) (*dbent.PendingAuthSession, *accountCredentialDraftState, error) {
	if strings.TrimSpace(reference) == "" {
		return nil, nil, ErrPendingAuthSessionNotFound
	}
	session, err := s.getBrowserSession(ctx, hashPendingAuthCode(reference))
	if err != nil {
		return nil, nil, err
	}
	state, err := readAccountCredentialDraft(session, binding)
	return session, state, err
}

func (s *AuthPendingIdentityService) PreviewAccountCredentialDraft(ctx context.Context, reference, binding string) (*AccountCredentialDraft, error) {
	session, state, err := s.accountCredentialDraft(ctx, reference, binding)
	if err != nil {
		return nil, err
	}
	if session.ConsumedAt != nil {
		return nil, ErrPendingAuthSessionConsumed
	}
	return &AccountCredentialDraft{Reference: reference, Platform: state.Platform, Type: state.Type, ExpiresAt: session.ExpiresAt, Revision: state.Revision}, nil
}

// ResolveAccountCredentialDraft is internal to preview/review services. The
// returned credentials must never be serialized as a handler response.
func (s *AuthPendingIdentityService) ResolveAccountCredentialDraft(ctx context.Context, reference, binding string) (*Account, string, error) {
	session, state, err := s.accountCredentialDraft(ctx, reference, binding)
	if err != nil {
		return nil, "", err
	}
	if session.ConsumedAt != nil {
		return nil, "", ErrPendingAuthSessionConsumed
	}
	return credentialDraftAccount(state), state.Revision, nil
}

// ReviewAccountCredentialDraft binds confirmation to the exact non-secret
// configuration reviewed by the operator. A new review invalidates older tabs.
func (s *AuthPendingIdentityService) ReviewAccountCredentialDraft(ctx context.Context, reference, binding, revision, digest string) (*AccountCredentialDraft, error) {
	if digest == "" {
		return nil, ErrAccountCredentialDraftConflict
	}
	transaction, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	_, err = transaction.PendingAuthSession.Update().Where(pendingauthsession.SessionTokenEQ(hashPendingAuthCode(reference)), pendingauthsession.BrowserSessionKeyEQ(binding)).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return nil, err
	}
	pending := NewAuthPendingIdentityService(transaction.Client())
	session, state, err := pending.accountCredentialDraft(ctx, reference, binding)
	if err != nil {
		return nil, err
	}
	if session.ConsumedAt != nil {
		return nil, ErrPendingAuthSessionConsumed
	}
	credentialRevision := ExpectedCredentialRevision(ctx)
	sameReview := state.ReviewDigest == digest && state.ReviewedCredentialRevision == credentialRevision
	if state.Revision != revision && !sameReview {
		return nil, ErrAccountCredentialDraftConflict
	}
	if !sameReview {
		state.Revision, err = randomOpaqueToken(24)
		if err != nil {
			return nil, err
		}
		state.ReviewDigest = digest
		state.ReviewedCredentialRevision = credentialRevision
		data, err := accountCredentialDraftData(*state)
		if err != nil {
			return nil, err
		}
		if err := transaction.PendingAuthSession.UpdateOneID(session.ID).SetLocalFlowState(data).Exec(ctx); err != nil {
			return nil, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return &AccountCredentialDraft{Reference: reference, Platform: state.Platform, Type: state.Type, ExpiresAt: session.ExpiresAt, Revision: state.Revision}, nil
}

func (s *AuthPendingIdentityService) CancelAccountCredentialDraft(ctx context.Context, reference, binding string) error {
	session, state, err := s.accountCredentialDraft(ctx, reference, binding)
	if err != nil {
		return err
	}
	if state.Cancelled {
		return nil
	}
	if session.ConsumedAt != nil {
		return ErrPendingAuthSessionConsumed
	}
	state.Credentials, state.Cancelled = nil, true
	data, err := accountCredentialDraftData(*state)
	if err != nil {
		return err
	}
	changed, err := s.entClient.PendingAuthSession.Update().Where(
		pendingauthsession.IDEQ(session.ID), pendingauthsession.ConsumedAtIsNil(), pendingauthsession.ExpiresAtGT(time.Now().UTC()),
	).SetConsumedAt(time.Now()).SetLocalFlowState(data).Save(ctx)
	if err != nil {
		return err
	}
	if changed == 0 {
		_, current, err := s.accountCredentialDraft(ctx, reference, binding)
		if err != nil {
			return err
		}
		if !current.Cancelled {
			return ErrPendingAuthSessionConsumed
		}
	}
	return nil
}

// ConfirmAccountCredentialDraft keeps the claim, account write and retry receipt
// in one transaction. create must use the transaction in its context for every
// database write and must defer external effects until this method succeeds.
// submission is a digest of the complete reviewed request, not a client chosen ID.
func (s *AuthPendingIdentityService) ConfirmAccountCredentialDraft(ctx context.Context, reference, binding, revision, submission string, create func(context.Context, *Account) (int64, error)) (int64, error) {
	if strings.TrimSpace(submission) == "" || create == nil {
		return 0, ErrAccountCredentialDraftConflict
	}
	transaction := dbent.TxFromContext(ctx)
	ownsTransaction := transaction == nil
	var err error
	if ownsTransaction {
		transaction, err = s.entClient.Tx(ctx)
		if err != nil {
			return 0, err
		}
		defer func() { _ = transaction.Rollback() }()
	}
	commit := func() error {
		if ownsTransaction {
			return transaction.Commit()
		}
		return nil
	}
	transactionContext := dbent.NewTxContext(ctx, transaction)
	pending := NewAuthPendingIdentityService(transaction.Client())
	// UPDATE obtains the row lock before reading: concurrent confirmations and
	// cancellation serialize on the same durable row, including across replicas.
	_, err = transaction.PendingAuthSession.Update().Where(
		pendingauthsession.SessionTokenEQ(hashPendingAuthCode(reference)),
		pendingauthsession.ProviderKeyEQ(identityTicketProviderKey), pendingauthsession.ProviderSubjectEQ(accountCredentialDraftPurpose),
		pendingauthsession.BrowserSessionKeyEQ(binding),
	).SetUpdatedAt(time.Now()).Save(transactionContext)
	if err != nil {
		return 0, err
	}
	session, state, err := pending.accountCredentialDraft(transactionContext, reference, binding)
	if err != nil {
		return 0, err
	}
	if state.Cancelled {
		return 0, ErrPendingAuthSessionConsumed
	}
	if state.Revision != revision {
		return 0, ErrAccountCredentialDraftConflict
	}
	if state.ReviewDigest != "" && state.ReviewDigest != submission {
		return 0, ErrAccountCredentialDraftConflict
	}
	if session.ConsumedAt != nil {
		if state.Submission != submission || state.AccountID <= 0 {
			return 0, ErrAccountCredentialDraftConflict
		}
		return state.AccountID, commit()
	}
	writeContext := transactionContext
	if state.ReviewedCredentialRevision != "" {
		writeContext = WithCredentialRevision(writeContext, state.ReviewedCredentialRevision)
	}
	accountID, err := create(writeContext, credentialDraftAccount(state))
	if err != nil {
		return 0, err
	}
	if accountID <= 0 {
		return 0, ErrAccountCredentialDraftConflict
	}
	if state.Egress != nil {
		if s.egressRuntime == nil {
			return 0, ErrProxyGroupUnavailable
		}
		if err := s.egressRuntime.confirmOAuthDraftBinding(transactionContext, state.Egress, accountID); err != nil {
			if state.Egress.ProxyID > 0 {
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(transactionContext), 2*time.Second)
				_, _ = s.egressRuntime.cache.DeleteBindingIfValue(cleanupCtx, state.Egress.GroupID, accountID, state.Egress.ProxyID)
				cancel()
			}
			return 0, err
		}
		selected := *state.Egress
		transaction.OnRollback(func(next dbent.Rollbacker) dbent.Rollbacker {
			return dbent.RollbackFunc(func(rollbackCtx context.Context, tx *dbent.Tx) error {
				err := next.Rollback(rollbackCtx, tx)
				if err == nil && selected.ProxyID > 0 {
					cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(rollbackCtx), 2*time.Second)
					defer cancel()
					_, _ = s.egressRuntime.cache.DeleteBindingIfValue(cleanupCtx, selected.GroupID, accountID, selected.ProxyID)
				}
				return err
			})
		})
	}
	state.Credentials, state.AccountID, state.Submission = nil, accountID, submission
	data, err := accountCredentialDraftData(*state)
	if err != nil {
		return 0, err
	}
	if err = transaction.PendingAuthSession.UpdateOneID(session.ID).SetConsumedAt(time.Now()).SetLocalFlowState(data).Exec(transactionContext); err != nil {
		return 0, err
	}
	if err = commit(); err != nil {
		return 0, err
	}
	return accountID, nil
}

// RunAccountWriteTransaction owns the entire import or batch, including draft
// receipts and proxy writes. Nested confirmations must not commit independently.
func (s *AuthPendingIdentityService) RunAccountWriteTransaction(ctx context.Context, write func(context.Context) error) error {
	if dbent.TxFromContext(ctx) != nil {
		return write(ctx)
	}
	transaction, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if err := write(dbent.NewTxContext(ctx, transaction)); err != nil {
		return err
	}
	return transaction.Commit()
}

// AfterAccountWriteCommit prevents privacy requests, probes and cache publication
// from observing a batch that may still roll back.
func AfterAccountWriteCommit(ctx context.Context, effect func()) {
	if transaction := dbent.TxFromContext(ctx); transaction != nil {
		transaction.OnCommit(func(next dbent.Committer) dbent.Committer {
			return dbent.CommitFunc(func(commitContext context.Context, transaction *dbent.Tx) error {
				if err := next.Commit(commitContext, transaction); err != nil {
					return err
				}
				effect()
				return nil
			})
		})
		return
	}
	effect()
}

func (s *AuthPendingIdentityService) SetEgressRuntime(runtime *ProxyGroupRuntime) {
	if s != nil {
		s.egressRuntime = runtime
	}
}
func credentialDraftAccount(state *accountCredentialDraftState) *Account {
	a := &Account{Platform: state.Platform, Type: state.Type, Credentials: state.Credentials}
	if state.Egress != nil {
		selected := *state.Egress
		a.ProxyGroupID = &selected.GroupID
		a.WorkspaceID = selected.WorkspaceID
		a.oauthEgress = &selected
		a.egress = &accountEgress{target: EgressTarget{URL: selected.URL, ProxyID: selected.ProxyID, Source: EgressSource(selected.Source), Resolved: true}}
	}
	return a
}

// ValidateDraftProxyChoice refuses client changes to the server-owned authorization route.
func ValidateDraftProxyChoice(account *Account, nodeID, groupID *int64) error {
	return validateOAuthSessionEgress(account.oauthEgress, nodeID, groupID)
}
