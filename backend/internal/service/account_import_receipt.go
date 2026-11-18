package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const StagedImportOperation = "staged-account-import"
const stagedReceiptVersion = 1

var (
	ErrStagedImportUnavailable    = infraerrors.ServiceUnavailable("STAGED_IMPORT_RECEIPT_UNAVAILABLE", "Staged import receipt store unavailable; retry with the same Idempotency-Key and request")
	ErrStagedImportOutcomeUnknown = infraerrors.ServiceUnavailable("STAGED_IMPORT_OUTCOME_UNKNOWN", "Staged import outcome unknown; retry with the same Idempotency-Key and request")
	ErrStagedImportLegacyOutcome  = infraerrors.Conflict("STAGED_IMPORT_LEGACY_OUTCOME_UNKNOWN", "An earlier import used this key; reconcile its outcome before proceeding")
	ErrStagedImportActorRequired  = infraerrors.Unauthorized("STAGED_IMPORT_ACTOR_REQUIRED", "An authenticated import actor is required")
	ErrStagedImportFailed         = infraerrors.InternalServer("STAGED_ACCOUNT_IMPORT_FAILED", "Staged import failed; retry with the same Idempotency-Key and request")
)

// StagedImportIdentity contains only trusted server context and an opaque key hash.
// Actor is deliberately NOT part of uniqueness or locking: a different actor
// using the same workspace/key must conflict, not start another import.
type StagedImportIdentity struct {
	WorkspaceID int64  `json:"workspace_id"`
	Operation   string `json:"operation"`
	KeyHash     string `json:"key_hash"`
	ActorID     int64  `json:"actor_id"`
}

type StagedImportReceipt struct {
	ActorID int64
	Version int
	Sealed  string
}

// All methods must use the caller's Ent transaction; there is no sql.DB fallback.
type StagedImportReceiptRepository interface {
	Lock(context.Context, StagedImportIdentity) error
	GetMode(context.Context, StagedImportIdentity) (*AccountImportKeyMode, error)
	InsertMode(context.Context, StagedImportIdentity, string) error
	Get(context.Context, StagedImportIdentity) (*StagedImportReceipt, error)
	LegacyExists(context.Context, StagedImportIdentity) (bool, error)
	LegacyFingerprint(context.Context, StagedImportIdentity) (string, bool, error)
	Insert(context.Context, StagedImportIdentity, StagedImportReceipt) error
}

// Fixed, bounded response schema: never store names, errors, account objects or credentials.
type StagedImportResult struct {
	StagedInactive bool `json:"staged_inactive"`
	ProxyCreated   int  `json:"proxy_created"`
	ProxyReused    int  `json:"proxy_reused"`
	ProxyFailed    int  `json:"proxy_failed"`
	AccountCreated int  `json:"account_created"`
	AccountFailed  int  `json:"account_failed"`
}

type stagedImportEnvelope struct {
	Purpose       string               `json:"purpose"`
	Version       int                  `json:"version"`
	Identity      StagedImportIdentity `json:"identity"`
	Method        string               `json:"method"`
	PayloadDigest string               `json:"payload_digest"`
	Result        StagedImportResult   `json:"result"`
}

type StagedAccountImportService struct {
	repo      StagedImportReceiptRepository
	encryptor SecretEncryptor
}

func NewStagedAccountImportService(repo StagedImportReceiptRepository, encryptor SecretEncryptor) *StagedAccountImportService {
	return &StagedAccountImportService{repo: repo, encryptor: encryptor}
}

// Execute owns the commit boundary. It cannot be called from another transaction:
// reporting success before an outer commit would reintroduce the uncertain outcome.
// payload is the complete decoded request BEFORE any generated defaults/mutations.
func (s *StagedAccountImportService) Execute(ctx context.Context, pending *AuthPendingIdentityService, actorID int64, rawKey string, payload any, create func(context.Context) (int, error)) (StagedImportResult, bool, error) {
	var zero StagedImportResult
	if actorID <= 0 {
		return zero, false, ErrStagedImportActorRequired
	}
	key, err := NormalizeIdempotencyKey(rawKey)
	if err != nil {
		return zero, false, err
	}
	if key == "" {
		return zero, false, ErrIdempotencyKeyRequired
	}
	if s == nil || s.repo == nil || pending == nil || pending.entClient == nil || create == nil || dbent.TxFromContext(ctx) != nil {
		return zero, false, ErrStagedImportUnavailable
	}
	if err := RequirePersistentEncryption(s.encryptor); err != nil {
		return zero, false, err
	}
	id := StagedImportIdentity{WorkspaceID: domain.ResourceWorkspaceID(ctx), Operation: StagedImportOperation, KeyHash: HashIdempotencyKey(key), ActorID: actorID}
	if id.WorkspaceID <= 0 {
		return zero, false, ErrStagedImportActorRequired
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return zero, false, ErrIdempotencyInvalidPayload
	}
	digest := sha256.Sum256(raw)
	// Only the encrypted envelope may persist this digest. Never pass payload or
	// its unkeyed digest to the general coordinator, SQL, logging or error causes.
	fingerprint := hex.EncodeToString(digest[:])
	// Both modes reserve the SAME key before any business execution. This is a
	// separate durable transaction: even a rolled-back/abandoned attempt must
	// not allow a later request to silently switch to active legacy creation.
	if err := s.reserveMode(ctx, pending, id, AccountImportModeStaged, ""); err != nil {
		return zero, false, err
	}
	transaction, err := pending.entClient.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return zero, false, ErrStagedImportUnavailable
	}
	defer func() { _ = transaction.Rollback() }()
	txCtx := WithStagedAccountImport(dbent.NewTxContext(ctx, transaction))
	if err := s.repo.Lock(txCtx, id); err != nil {
		return zero, false, ErrStagedImportUnavailable
	}
	receipt, err := s.repo.Get(txCtx, id)
	if err != nil {
		return zero, false, ErrStagedImportUnavailable
	}
	if receipt != nil {
		if receipt.ActorID != id.ActorID {
			return zero, false, ErrIdempotencyKeyConflict
		}
		result, err := s.replay(id, fingerprint, *receipt)
		if err != nil {
			return zero, false, err
		}
		// This transaction made no changes. The receipt is already durable.
		return result, true, nil
	}
	// Old coordinator entries cannot prove atomic success. Fail closed rather
	// than silently reuse a legacy key after changing the implementation.
	legacy, err := s.repo.LegacyExists(txCtx, id)
	if err != nil {
		return zero, false, ErrStagedImportUnavailable
	}
	if legacy {
		return zero, false, ErrStagedImportLegacyOutcome
	}
	count, err := create(txCtx)
	if err != nil {
		if errors.Is(err, ErrEncryptionKeyRequired) {
			return zero, false, ErrEncryptionKeyRequired
		}
		return zero, false, ErrStagedImportFailed
	}
	if count < 1 || count > 1000 {
		return zero, false, ErrStagedImportFailed
	}
	result := StagedImportResult{StagedInactive: true, AccountCreated: count}
	envelope := stagedImportEnvelope{Purpose: StagedImportOperation, Version: stagedReceiptVersion, Identity: id, Method: "POST", PayloadDigest: fingerprint, Result: result}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return zero, false, ErrStagedImportUnavailable
	}
	sealed, err := s.encryptor.Encrypt(string(encoded))
	if err != nil {
		return zero, false, ErrStagedImportUnavailable
	}
	if err := s.repo.Insert(txCtx, id, StagedImportReceipt{ActorID: id.ActorID, Version: stagedReceiptVersion, Sealed: sealed}); err != nil {
		return zero, false, ErrStagedImportUnavailable
	}
	if err := transaction.Commit(); err != nil {
		// Even a connection error can mean COMMIT reached PostgreSQL. Do not
		// claim rollback, mark a second coordinator failed, or retry create here.
		return zero, false, ErrStagedImportOutcomeUnknown
	}
	return result, false, nil
}

func (s *StagedAccountImportService) replay(id StagedImportIdentity, fingerprint string, receipt StagedImportReceipt) (StagedImportResult, error) {
	var envelope stagedImportEnvelope
	if receipt.Version != stagedReceiptVersion {
		return StagedImportResult{}, ErrStagedImportUnavailable
	}
	plain, err := s.encryptor.Decrypt(receipt.Sealed)
	if err != nil || json.Unmarshal([]byte(plain), &envelope) != nil {
		return StagedImportResult{}, ErrStagedImportUnavailable
	}
	// SecretEncryptor has no AAD argument. Authenticate and check row identity
	// inside the GCM envelope instead, including its purpose/domain separator.
	if envelope.Version != stagedReceiptVersion || envelope.Purpose != StagedImportOperation || envelope.Method != "POST" || envelope.Identity != id {
		return StagedImportResult{}, ErrStagedImportUnavailable
	}
	if subtle.ConstantTimeCompare([]byte(envelope.PayloadDigest), []byte(fingerprint)) != 1 {
		return StagedImportResult{}, ErrIdempotencyKeyConflict
	}
	r := envelope.Result
	if !r.StagedInactive || r.AccountCreated < 1 || r.AccountCreated > 1000 || r.AccountFailed != 0 || r.ProxyCreated != 0 || r.ProxyReused != 0 || r.ProxyFailed != 0 {
		return StagedImportResult{}, ErrStagedImportUnavailable
	}
	return r, nil
}
