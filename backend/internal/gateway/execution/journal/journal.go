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
	Model, Price string
	PricingAt    time.Time
}

type AdmittedExecution struct {
	RequestID      string
	Scope          scope.Authorized
	AttemptBudget  int
	Deadline       time.Time
	Versions       Versions
	ReservationRef string
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
	ID, RequestID string
	Number        int
	AccountID     int64
	Acceptance    attempt.Acceptance
	Outcome       string
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
