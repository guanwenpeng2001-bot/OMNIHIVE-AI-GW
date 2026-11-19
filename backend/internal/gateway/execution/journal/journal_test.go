package journal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/stretchr/testify/require"
)

func TestLostAdmissionAcknowledgement(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	a := scope.Authorized{WorkspaceID: 1, ProjectID: 2, ActorID: 3, KeyID: 4, KeyOwnerID: 5, FundingUserID: 6, Version: 7}
	e := Admission{RequestID: NewID(), Kind: "request", Route: "/v1/responses", Model: "m", AttemptBudget: 2, Deadline: time.Now().Add(time.Minute), Versions: Versions{PricingAt: time.Now()}}
	m.ExpectQuery("WITH admitted").WillReturnError(errors.New("connection lost after commit"))
	rows := sqlmock.NewRows([]string{"request_id", "project_id", "workspace_id", "actor_id", "api_key_id", "owner_id", "funding_user_id", "authorization_version", "attempt_budget", "deadline", "model_version", "price_version", "pricing_at", "reservation_ref", "fingerprint", "kind", "model", "legacy_id"}).
		AddRow(e.RequestID, a.ProjectID, a.WorkspaceID, a.ActorID, a.KeyID, a.KeyOwnerID, a.FundingUserID, a.Version, e.AttemptBudget, e.Deadline, "", "", e.Versions.PricingAt, e.RequestID, nil, e.Kind, e.Model, "")
	m.ExpectQuery("SELECT request_id,project_id").WillReturnRows(rows)
	got, replay, err := New(db).Admit(scope.With(context.Background(), a), e)
	require.NoError(t, err)
	require.False(t, replay)
	require.Equal(t, e.RequestID, got.RequestID)
	require.Equal(t, a, got.Scope)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestRegistrationFingerprint(t *testing.T) {
	left, err := Register("k", []byte(`{"input":[{"a":0,"b":9007199254740993}]}`))
	require.NoError(t, err)
	right, err := Register("k", []byte(`{ "input": [ {"b":9007199254740993,"a":0} ] }`))
	require.NoError(t, err)
	require.Equal(t, left, right)
	different, err := Register("k", []byte(`{"input":[{"b":9007199254740992,"a":0}]}`))
	require.NoError(t, err)
	require.NotEqual(t, left, different)
	_, err = Register("k", []byte(`{"a":0,"a":1}`))
	require.ErrorIs(t, err, ErrRegistration)
}

func BenchmarkRegistration(b *testing.B) {
	body := []byte(`{"model":"model","input":[{"role":"user","content":"hello"}],"stream":true}`)
	for _, key := range []string{"", "submit"} {
		b.Run(map[bool]string{true: "unkeyed", false: "keyed"}[key == ""], func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Register(key, body); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
