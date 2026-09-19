package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// AccountCatalogReview binds an operator's approval to a full preview, including
// the release and capability evidence. It is an optimistic concurrency token,
// not an authorization credential. Ordinary scheduled refreshes do not use it.
type AccountCatalogReview struct {
	AccountID int64 `json:"account_id"`
	AccountModelCatalogBaseline
	CredentialID       int64                    `json:"credential_account_id"`
	CredentialRevision int64                    `json:"credential_identity_revision,string"`
	Release            AccountCapabilityRelease `json:"release"`
	Digest             string                   `json:"digest"`
}

type catalogReviewKey struct{}

func (s *AccountTestService) PreviewAccountModelCatalog(ctx context.Context, account *Account) (*UpstreamModelCatalog, error) {
	if account == nil {
		return nil, newUpstreamModelSyncConfigError("Account is required", nil)
	}
	baseline := account.ModelCatalogBaseline()
	if account.IsShadow() {
		parent, err := resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return nil, err
		}
		baseline.CredentialAccountID = parent.ID
		baseline.CredentialIdentityRevision = parent.ModelCatalogState().IdentityRevision
	}
	catalog, err := s.SyncUpstreamModelCatalog(ctx, account)
	if err != nil {
		var partial *UpstreamModelSyncError
		if errors.As(err, &partial) && partial.PartialCatalog != nil {
			return partial.PartialCatalog, nil
		}
		return nil, err
	}
	catalog.Review, err = newAccountCatalogReview(account.ID, baseline, catalog)
	return catalog, err
}

func (s *AccountTestService) ApplyReviewedAccountModelCatalog(ctx context.Context, account *Account, review *AccountCatalogReview) (*UpstreamModelCatalog, error) {
	if account == nil || review == nil || review.AccountID != account.ID || review.Digest == "" {
		return nil, ErrAccountModelCatalogConflict
	}
	return s.ApplyAccountModelCatalog(context.WithValue(ctx, catalogReviewKey{}, review), account, nil)
}

func newAccountCatalogReview(id int64, baseline AccountModelCatalogBaseline, catalog *UpstreamModelCatalog) (*AccountCatalogReview, error) {
	review := &AccountCatalogReview{AccountID: id, AccountModelCatalogBaseline: baseline,
		CredentialID: baseline.CredentialAccountID, CredentialRevision: baseline.CredentialIdentityRevision,
		Release: AccountCapabilityRelease{SchemaVersion: catalog.SchemaVersion, EnrichmentVersion: catalog.EnrichmentVersion, SourceGeneration: catalog.SourceGeneration, SourceVersions: catalog.SourceVersions}}
	// Observation timestamps naturally change on the second discovery. Everything
	// else, including provenance, exclusions and the reviewed diff, must match.
	encoded, err := json.Marshal(struct {
		Review  *AccountCatalogReview
		Catalog *UpstreamModelCatalog
	}{review, catalog})
	if err != nil {
		return nil, err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err = decoder.Decode(&value); err != nil {
		return nil, err
	}
	stripCatalogObservationTimes(value)
	encoded, err = json.Marshal(value)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	review.Digest = hex.EncodeToString(hash[:])
	return review, nil
}

func stripCatalogObservationTimes(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			switch key {
			case "observed_at", "synced_at", "evidence_observed_at":
				delete(v, key)
			default:
				stripCatalogObservationTimes(child)
			}
		}
	case []any:
		for _, child := range v {
			stripCatalogObservationTimes(child)
		}
	}
}
