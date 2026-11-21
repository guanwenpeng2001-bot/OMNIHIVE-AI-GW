package service

import (
	"context"
	"database/sql"
	"strconv"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

const (
	AccountImportModeStaged = "staged"
	AccountImportModeLegacy = "legacy"
)

var (
	ErrAccountImportModeUnavailable    = infraerrors.ServiceUnavailable("ACCOUNT_IMPORT_KEY_UNAVAILABLE", "Import key store unavailable; retry with the same Idempotency-Key and mode")
	ErrAccountImportModeOutcomeUnknown = infraerrors.ServiceUnavailable("ACCOUNT_IMPORT_KEY_OUTCOME_UNKNOWN", "Import key reservation outcome unknown; retry with the same Idempotency-Key and mode")
)

type AccountImportKeyMode struct {
	ActorID int64
	Mode    string
}

// ReserveLegacyKey must run BEFORE the generic coordinator, including its replay
// and fail-open paths. The reservation contains no credentials or payload digest.
// Missing keys retain legacy behavior (coordinator ObserveOnly/RequireKey policy).
func (s *StagedAccountImportService) ReserveLegacyKey(ctx context.Context, pending *AuthPendingIdentityService, actorID int64, opts IdempotencyExecuteOptions) error {
	key, err := NormalizeIdempotencyKey(opts.IdempotencyKey)
	if err != nil {
		return err
	}
	if key == "" {
		return nil
	}
	if actorID <= 0 || domain.ResourceWorkspaceID(ctx) <= 0 {
		return ErrStagedImportActorRequired
	}
	id := StagedImportIdentity{WorkspaceID: domain.ResourceWorkspaceID(ctx), Operation: StagedImportOperation, KeyHash: HashIdempotencyKey(key), ActorID: actorID}
	// Match the existing coordinator's contract when adopting a historical key.
	// This digest stays in memory; the mode table never stores request digests.
	actorScope := domain.WorkspaceActorScope(ctx, "admin:"+strconv.FormatInt(actorID, 10))
	fingerprint, err := BuildIdempotencyFingerprint(opts.Method, opts.Route, actorScope, opts.Payload)
	if err != nil {
		return ErrIdempotencyInvalidPayload
	}
	return s.reserveMode(ctx, pending, id, AccountImportModeLegacy, fingerprint)
}

// reserveMode serializes ALL contenders, not just the staged account transaction.
// The immutable, committed mode row replaces the lock after this short transaction
// ends. No business callback runs before its commit succeeds. Existing same-mode
// rows are not execution receipts: legacy retries still use the old coordinator.
func (s *StagedAccountImportService) reserveMode(ctx context.Context, pending *AuthPendingIdentityService, id StagedImportIdentity, mode, legacyFingerprint string) error {
	if s == nil || s.repo == nil || pending == nil || pending.entClient == nil || dbent.TxFromContext(ctx) != nil {
		return ErrAccountImportModeUnavailable
	}
	tx, err := pending.entClient.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ErrAccountImportModeUnavailable
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := s.repo.Lock(txCtx, id); err != nil {
		return ErrAccountImportModeUnavailable
	}
	occupied, err := s.repo.GetMode(txCtx, id)
	if err != nil {
		return ErrAccountImportModeUnavailable
	}
	if occupied != nil {
		if occupied.ActorID != id.ActorID || occupied.Mode != mode {
			return ErrIdempotencyKeyConflict
		}
		return nil // Already committed and immutable; read transaction can roll back.
	}
	// Check for earlier receipt drafts under the same lock too. Never let a
	// missing mode row downgrade a previously committed staged import to legacy.
	receipt, err := s.repo.Get(txCtx, id)
	if err != nil {
		return ErrAccountImportModeUnavailable
	}
	if receipt != nil {
		if mode != AccountImportModeStaged || receipt.ActorID != id.ActorID {
			return ErrIdempotencyKeyConflict
		}
	} else if mode == AccountImportModeStaged {
		legacy, err := s.repo.LegacyExists(txCtx, id)
		if err != nil {
			return ErrAccountImportModeUnavailable
		}
		if legacy {
			return ErrStagedImportLegacyOutcome
		}
	} else {
		// Do not let an invalid retry/other actor claim an old coordinator key
		// before the original caller can replay it. Check while holding the same
		// shared key lock, before persisting any mode ownership.
		stored, exists, err := s.repo.LegacyFingerprint(txCtx, id)
		if err != nil {
			return ErrAccountImportModeUnavailable
		}
		if exists && stored != legacyFingerprint {
			return ErrIdempotencyKeyConflict
		}
	}
	if err := s.repo.InsertMode(txCtx, id, mode); err != nil {
		return ErrAccountImportModeUnavailable
	}
	if err := tx.Commit(); err != nil {
		return ErrAccountImportModeOutcomeUnknown
	}
	return nil
}
