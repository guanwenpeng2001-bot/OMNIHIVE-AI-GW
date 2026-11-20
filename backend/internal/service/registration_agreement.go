package service

import (
	"context"
	"strings"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

var ErrRegistrationAgreement = infraerrors.BadRequest("LOGIN_AGREEMENT_REQUIRED", "Review and accept the current registration documents")

type SignupAgreement struct {
	Accepted bool   `json:"login_agreement_accepted"`
	Revision string `json:"login_agreement_revision"`
}

type signupAgreementKey struct{}

func (a SignupAgreement) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, signupAgreementKey{}, a)
}

func signupAgreementFromContext(ctx context.Context) *SignupAgreement {
	a, _ := ctx.Value(signupAgreementKey{}).(SignupAgreement)
	return &a
}

func (a SignupAgreement) AcceptedRevision(settings map[string]string) (string, error) {
	if settings[SettingKeyLoginAgreementEnabled] != "true" {
		return "", nil
	}
	documents := parseLoginAgreementDocuments(settings[SettingKeyLoginAgreementDocuments])
	if !loginAgreementReady(documents) {
		return "", ErrRegistrationAgreement
	}
	updatedAt := strings.TrimSpace(settings[SettingKeyLoginAgreementUpdatedAt])
	if updatedAt == "" {
		updatedAt = defaultLoginAgreementDate
	}
	current := buildLoginAgreementRevision(updatedAt, documents)
	if !a.Accepted || a.Revision != current {
		return "", ErrRegistrationAgreement
	}
	return current, nil
}
