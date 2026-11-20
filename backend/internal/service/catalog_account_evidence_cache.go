package service

import (
	"context"
	"sync"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

const catalogAccountEvidenceCapacity = 4096

// Generation-keyed per-account catalog evidence. Database triggers advance the
// catalog generation on every accounts/groups/account_groups/routes write, so
// an unchanged generation proves the evidence is unchanged; a live generation
// probe plus a generation-keyed hit is exactly as fresh as re-reading the row.
// Entries are shared read-only across requests: binding computations never
// mutate the account, and unprepared accounts carry no private memo state.
type catalogAccountEvidenceCache struct {
	mu      sync.Mutex
	entries map[catalogAccountEvidenceKey]*Account
	missing map[catalogAccountEvidenceKey]bool
}

type catalogAccountEvidenceKey struct {
	workspace  int64
	scoped     bool
	account    int64
	generation int64
}

var dispatchCatalogAccountEvidence catalogAccountEvidenceCache

func init() {
	dispatchCatalogAccountEvidence.entries = make(map[catalogAccountEvidenceKey]*Account)
	dispatchCatalogAccountEvidence.missing = make(map[catalogAccountEvidenceKey]bool)
}

func (cache *catalogAccountEvidenceCache) lookup(key catalogAccountEvidenceKey) (*Account, bool, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if account, ok := cache.entries[key]; ok {
		return account, false, true
	}
	if cache.missing[key] {
		return nil, true, true
	}
	return nil, false, false
}

func (cache *catalogAccountEvidenceCache) store(key catalogAccountEvidenceKey, account *Account) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries)+len(cache.missing) >= catalogAccountEvidenceCapacity {
		clear(cache.entries)
		clear(cache.missing)
	}
	if account == nil {
		cache.missing[key] = true
		return
	}
	cache.entries[key] = account
}

func (cache *catalogAccountEvidenceCache) flush() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	clear(cache.entries)
	clear(cache.missing)
}

// modelCatalogAccountEvidence serves GetModelCatalogAccount through the
// generation-keyed store. A frozen request revision supplies the generation
// for free; otherwise the primary database is probed once, which is still a
// live freshness check (the send boundary keeps its "current evidence"
// guarantee because any evidence write advances the generation).
func modelCatalogAccountEvidence(ctx context.Context, repository AccountRepository, id int64) (*Account, error) {
	reader, ok := repository.(ModelCatalogAccountRepository)
	if !ok {
		return repository.GetByID(ctx, id)
	}
	generation, authoritative, err := catalogEvidenceGeneration(ctx, repository)
	if err != nil {
		return nil, err
	}
	if !authoritative {
		return reader.GetModelCatalogAccount(ctx, id)
	}
	workspace, scoped := domain.WorkspaceScope(ctx)
	key := catalogAccountEvidenceKey{workspace: workspace, scoped: scoped, account: id, generation: generation}
	if account, missing, hit := dispatchCatalogAccountEvidence.lookup(key); hit {
		if missing {
			return nil, nil
		}
		return account, nil
	}
	account, err := reader.GetModelCatalogAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	dispatchCatalogAccountEvidence.store(key, account)
	return account, nil
}

// catalogEvidenceGeneration prefers the request's frozen authoritative
// revision, then the generation-fenced access snapshot attached at
// authentication, and only otherwise probes the primary database. Consumers
// that must observe mid-request writes keep their own live reads (admission
// validate and the send-boundary revision compare stay authoritative).
func catalogEvidenceGeneration(ctx context.Context, repository any) (int64, bool, error) {
	if revision, ok := ctx.Value(visibleModelRevisionKey{}).(visibleModelRevision); ok && revision.authoritative {
		return revision.generation, true, nil
	}
	if snapshot, ok := ctx.Value(modelAccessSnapshotKey{}).(ModelAccessSnapshot); ok && snapshot.RevisionKnown {
		if _, readerOK := repository.(ModelCatalogGenerationReader); readerOK {
			return snapshot.Generation, true, nil
		}
	}
	return modelCatalogGeneration(ctx, repository, "catalog")
}
