//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignupAgreementRequiresCurrentPublishedRevision(t *testing.T) {
	documents, err := marshalLoginAgreementDocuments([]LoginAgreementDocument{
		{ID: "terms", Title: "Terms", ContentMD: "Operator terms"},
		{ID: "privacy-policy", Title: "Privacy", ContentMD: "Operator privacy notice"},
	})
	require.NoError(t, err)
	settings := map[string]string{
		SettingKeyLoginAgreementEnabled:   "true",
		SettingKeyLoginAgreementDocuments: documents,
		SettingKeyLoginAgreementUpdatedAt: "2026-11-21",
	}
	revision := buildLoginAgreementRevision("2026-11-21", parseLoginAgreementDocuments(documents))
	accepted, err := (SignupAgreement{Accepted: true, Revision: revision}).AcceptedRevision(settings)
	require.NoError(t, err)
	require.Equal(t, revision, accepted)
	for name, agreement := range map[string]SignupAgreement{
		"unchecked": {Revision: revision},
		"stale":     {Accepted: true, Revision: "previous"},
		"missing":   {},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := agreement.AcceptedRevision(settings)
			require.ErrorIs(t, err, ErrRegistrationAgreement)
		})
	}
	settings[SettingKeyLoginAgreementDocuments], err = marshalLoginAgreementDocuments(defaultLoginAgreementDocuments())
	require.NoError(t, err)
	_, err = (SignupAgreement{Accepted: true, Revision: revision}).AcceptedRevision(settings)
	require.ErrorIs(t, err, ErrRegistrationAgreement)
}
