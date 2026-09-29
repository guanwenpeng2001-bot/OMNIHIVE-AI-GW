package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

// Gateway claims use the existing durable idempotency repository, but never
// reclaim an execution. This prevents local duplicate dispatch, NOT exactly-once
// upstream side effects: a crash cannot undo a request already sent upstream.
// Rollbacks must retain these rows and this admission check. Never purge this
// namespace to roll back routing; older cleanup jobs also respect ExpiresAt.
var gatewayGenerationRetainUntil = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

const GatewayGenerationMaxResponseBytes = 8 * 1024 * 1024

type GatewayGenerationScope struct {
	WorkspaceID int64
	APIKeyID    int64  // Stable key identity, never its credential or hash.
	Action      string // Canonical inbound action including its response protocol.
}

// Only response bytes and scalar billing evidence are retained. Do not pass
// hydrated API keys, accounts, request headers or settlement commands here.
type GatewayGenerationResponse struct {
	Status       int                  `json:"status"`
	ContentType  string               `json:"content_type,omitempty"`
	Body         []byte               `json:"body,omitempty"`
	State        string               `json:"state"`
	ExecutionID  string               `json:"execution_id,omitempty"`
	ReceiptKey   UsageSettlementKey   `json:"receipt_key"`
	ReceiptState UsageSettlementState `json:"receipt_state,omitempty"`
}

type GatewayGenerationClaim struct {
	coordinator *IdempotencyCoordinator
	recordID    int64
	Replay      *GatewayGenerationResponse
}

var ErrGatewayGenerationScope = infraerrors.BadRequest("IDEMPOTENCY_SCOPE_REQUIRED", "authenticated key, workspace and action are required")

// BeginGatewayGeneration binds the key to a body digest, rather than putting
// the digest in the unique key (which would silently allow same-key/different-body).
// Body is the original wire body; whitespace differences intentionally conflict.
func (c *IdempotencyCoordinator) BeginGatewayGeneration(ctx context.Context, scope GatewayGenerationScope, rawKey string, body []byte) (*GatewayGenerationClaim, error) {
	key, err := NormalizeIdempotencyKey(rawKey)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	if scope.APIKeyID <= 0 || scope.WorkspaceID < 0 || scope.Action == "" {
		return nil, ErrGatewayGenerationScope
	}
	if c == nil || c.repo == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	scopeJSON, _ := json.Marshal(scope)
	record := &IdempotencyRecord{
		Scope:              "gateway-generation:" + HashIdempotencyKey(string(scopeJSON)),
		IdempotencyKeyHash: HashIdempotencyKey(key),
		RequestFingerprint: HashIdempotencyKey(string(body)),
		Status:             IdempotencyStatusProcessing,
		ExpiresAt:          gatewayGenerationRetainUntil,
	}
	owner, err := c.repo.CreateProcessing(ctx, record)
	if err != nil {
		return nil, ErrIdempotencyStoreUnavail.WithCause(err)
	}
	if owner {
		if record.ID == 0 {
			return nil, ErrIdempotencyStoreUnavail
		}
		return &GatewayGenerationClaim{coordinator: c, recordID: record.ID}, nil
	}
	existing, err := c.repo.GetByScopeAndKeyHash(ctx, record.Scope, record.IdempotencyKeyHash)
	if err != nil {
		return nil, ErrIdempotencyStoreUnavail.WithCause(err)
	}
	if existing == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	if existing.RequestFingerprint != record.RequestFingerprint {
		return nil, ErrIdempotencyKeyConflict
	}
	if existing.Status == IdempotencyStatusProcessing {
		// A crashed process is indistinguishable from a still-running upstream.
		// No timeout, config change, or retry can acquire this claim again.
		return nil, ErrIdempotencyInProgress
	}
	if existing.Status != IdempotencyStatusSucceeded || existing.ResponseBody == nil {
		return nil, infraerrors.Conflict("IDEMPOTENCY_OUTCOME_UNKNOWN", "original execution outcome is unknown; automatic regeneration is disabled")
	}
	var response GatewayGenerationResponse
	if err := json.Unmarshal([]byte(*existing.ResponseBody), &response); err != nil || response.State == "" {
		return nil, ErrIdempotencyStoreUnavail
	}
	return &GatewayGenerationClaim{Replay: &response}, nil
}

// Complete persists even after client cancellation. It does not settle usage;
// receipt/outbox recovery owns that, and replay never invokes a billing producer.
// Storage failure leaves the claim in place, so it cannot trigger regeneration.
func (claim *GatewayGenerationClaim) Complete(ctx context.Context, response GatewayGenerationResponse) error {
	if claim == nil || claim.coordinator == nil || claim.recordID == 0 || claim.Replay != nil {
		return fmt.Errorf("generation claim is not owned")
	}
	if response.State == "" {
		response.State = "completed"
	}
	if len(response.Body) > GatewayGenerationMaxResponseBytes || generationResponseContainsCredentials(response.Body) {
		response.State = "response_unavailable"
		response.Body = nil
	}
	if response.State != "completed" {
		response.Body = nil
	}
	stored, err := json.Marshal(response)
	if err != nil {
		return err
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return claim.coordinator.repo.MarkSucceeded(persistCtx, claim.recordID, response.Status, string(stored), gatewayGenerationRetainUntil)
}

func generationResponseContainsCredentials(body []byte) bool {
	extraKeys := []string{"api_key", "authorization", "x-api-key", "secret"}
	// Compare structured values, not reserialized bytes: log redaction normalizes
	// JSON whitespace/order/numbers, while a replay must preserve original bytes.
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&decoded) == nil {
		wrapped := map[string]any{"response": decoded}
		if !reflect.DeepEqual(wrapped, logredact.RedactMap(wrapped, extraKeys...)) {
			return true
		}
	}
	// Prefix forces text-mode checking, also recognizing credential patterns in
	// generated text without normalizing otherwise innocuous response JSON.
	text := strings.TrimSpace("response: " + string(body))
	return logredact.RedactText(text, extraKeys...) != text
}
