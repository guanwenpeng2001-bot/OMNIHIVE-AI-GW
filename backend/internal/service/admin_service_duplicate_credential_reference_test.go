package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type credentialReferenceDuplicateRepo struct {
	*upstreamBillingProbeAccountRepo
}

func (r *credentialReferenceDuplicateRepo) CreateWithAccountGroups(ctx context.Context, account *Account, _ []AccountGroup) error {
	return r.Create(ctx, account)
}

// A referenced secret stays bound to its source account: the reviewed duplicate
// starts inline with the resolved plaintext and never carries the reference.
func TestDuplicateAccountStripsCredentialReference(t *testing.T) {
	ctx := context.Background()
	repo := &credentialReferenceDuplicateRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: make(map[int64]*Account)}}
	svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}
	source := &Account{
		Name:     "referenced-source",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":                    "sk-resolved-plaintext",
			CredentialSecretReferenceKey: "ref-bound-to-source",
			CredentialSecretVersionKey:   int64(3),
		},
	}
	require.NoError(t, repo.Create(ctx, source))

	duplicate, err := svc.DuplicateAccount(ctx, source.ID, "admin:1", "")

	require.NoError(t, err)
	require.NotEqual(t, source.ID, duplicate.ID)
	require.Equal(t, "sk-resolved-plaintext", duplicate.Credentials["api_key"])
	require.NotContains(t, duplicate.Credentials, CredentialSecretReferenceKey)
	require.NotContains(t, duplicate.Credentials, CredentialSecretVersionKey)
}

func TestStripCredentialReferenceKeys(t *testing.T) {
	credentials := map[string]any{
		"api_key":                    "sk-keep",
		"base_url":                   "https://example.invalid",
		CredentialSecretReferenceKey: "ref",
		CredentialSecretVersionKey:   int64(1),
	}
	StripCredentialReferenceKeys(credentials)
	require.Equal(t, map[string]any{"api_key": "sk-keep", "base_url": "https://example.invalid"}, credentials)
}
