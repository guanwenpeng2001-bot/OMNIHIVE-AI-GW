package journal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
)

var (
	ErrScope        = errors.New("execution scope is unavailable or changed")
	ErrConflict     = errors.New("idempotency key was used with different input")
	ErrReplay       = errors.New("execution already admitted")
	ErrDispatch     = errors.New("dispatch permit unavailable")
	ErrRegistration = errors.New("invalid execution idempotency key or payload")
)

type Versions struct {
	Model     string    `json:"model"`
	Price     string    `json:"price"`
	PricingAt time.Time `json:"pricing_at"`
}

type AdmittedExecution struct {
	RequestID      string           `json:"request_id"`
	Scope          scope.Authorized `json:"-"`
	AttemptBudget  int              `json:"attempt_budget"`
	Deadline       time.Time        `json:"deadline"`
	Versions       Versions         `json:"versions"`
	ReservationRef string           `json:"reservation_ref"`
}

// MarshalJSON projects server-resolved identities as opaque decimal strings.
func (e AdmittedExecution) MarshalJSON() ([]byte, error) {
	type fields AdmittedExecution
	type frozenScope struct {
		WorkspaceID   int64 `json:"workspace_id,string"`
		ProjectID     int64 `json:"project_id,string"`
		ActorID       int64 `json:"actor_id,string"`
		KeyID         int64 `json:"api_key_id,string"`
		KeyOwnerID    int64 `json:"key_owner_id,string"`
		FundingUserID int64 `json:"funding_user_id,string"`
		Version       int64 `json:"authorization_version,string"`
	}
	s := e.Scope
	return json.Marshal(struct {
		fields
		Scope frozenScope `json:"scope"`
	}{fields(e), frozenScope{s.WorkspaceID, s.ProjectID, s.ActorID, s.KeyID, s.KeyOwnerID, s.FundingUserID, s.Version}})
}

type Admission struct {
	RequestID, Kind, Route, Model, LegacyID string
	AttemptBudget                           int
	Deadline                                time.Time
	Versions                                Versions
	Registration                            *Registration
}

type Registration struct{ key, payload [32]byte }

func Register(key string, body []byte) (*Registration, error) {
	if key == "" {
		return nil, nil
	}
	if len(key) > 256 || !utf8.Valid(body) || !json.Valid(body) {
		return nil, ErrRegistration
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	value, err := decodeValue(dec)
	if err != nil {
		return nil, ErrRegistration
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, ErrRegistration
	}
	return &Registration{key: sha256.Sum256([]byte(key)), payload: sha256.Sum256(canonical)}, nil
}

// A keyed request must have an unambiguous representation across the native JSON decoders.
func decodeValue(dec *json.Decoder) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		obj := map[string]any{}
		for dec.More() {
			name, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key := name.(string)
			if _, exists := obj[key]; exists {
				return nil, ErrRegistration
			}
			obj[key], err = decodeValue(dec)
			if err != nil {
				return nil, err
			}
		}
		_, err = dec.Token()
		return obj, err
	case json.Delim('['):
		values := make([]any, 0)
		for dec.More() {
			v, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
		}
		_, err = dec.Token()
		return values, err
	default:
		return token, nil
	}
}

type Attempt struct {
	ID         string             `json:"id"`
	RequestID  string             `json:"request_id"`
	Number     int                `json:"ordinal"`
	AccountID  int64              `json:"account_id,string,omitempty"`
	Acceptance attempt.Acceptance `json:"acceptance"`
	Outcome    string             `json:"outcome"`
}

type Store interface {
	Admit(context.Context, Admission) (AdmittedExecution, bool, error)
	Dispatch(context.Context, AdmittedExecution, int, int64) (Attempt, error)
	Observe(context.Context, Attempt) error
	Decision(context.Context, string, attempt.AttemptDecision) error
	Finish(context.Context, string, lifecycle.State, bool) error
}

type Reauthorize func(context.Context, scope.Authorized, string) error
type storeKey struct{}
type authorizerKey struct{}

func WithStore(ctx context.Context, s Store) context.Context {
	return context.WithValue(ctx, storeKey{}, s)
}
func From(ctx context.Context) Store { s, _ := ctx.Value(storeKey{}).(Store); return s }
func WithReauthorization(ctx context.Context, a Reauthorize) context.Context {
	return context.WithValue(ctx, authorizerKey{}, a)
}
func Authorize(ctx context.Context, s scope.Authorized, model string) error {
	a, _ := ctx.Value(authorizerKey{}).(Reauthorize)
	if a == nil {
		return ErrScope
	}
	return a(ctx, s, model)
}

func NewID() string { return uuid.NewString() }
