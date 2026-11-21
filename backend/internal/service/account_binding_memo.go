package service

import (
	"context"
	"sync"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/xai"
)

const accountBindingMemoCapacity = 256

// Owned by immutable catalog read evidence, never by an authorization result.
// The enclosing catalog snapshot is keyed by workspace and primary-DB generation
// and validated again before publishing. Unprepared forwarding accounts bypass it.
type accountBindingMemo struct {
	mu         sync.Mutex
	generation int64
	entries    map[accountBindingMemoKey]accountBindingMemoValue
}

type accountBindingMemoKey struct {
	model, endpoint, finalModel, dispatchModel, registry string
	compact, thinking, thinkingKnown, endpointOnly       bool
	member                                               int
	runtime                                              uint64
	credential                                           *Account
}

type accountBindingMemoValue struct {
	model string
	ok    bool
}

func (memo *accountBindingMemo) resolve(generation int64, key accountBindingMemoKey, compute func() accountBindingMemoValue) accountBindingMemoValue {
	memo.mu.Lock()
	defer memo.mu.Unlock()
	if memo.generation != generation {
		memo.entries = nil
		memo.generation = generation
	}
	if value, ok := memo.entries[key]; ok {
		return value
	}
	value := compute()
	if len(memo.entries) >= accountBindingMemoCapacity {
		clear(memo.entries)
	}
	if memo.entries == nil {
		memo.entries = make(map[accountBindingMemoKey]accountBindingMemoValue)
	}
	memo.entries[key] = value
	return value
}

func accountBindingComputation(ctx context.Context, account *Account, model string, options AccountModelBindingOptions, member int, endpointOnly bool, compute func() accountBindingMemoValue) accountBindingMemoValue {
	if ctx == nil || account.modelCatalogReadEvidence == nil {
		return compute()
	}
	revision, ok := ctx.Value(visibleModelRevisionKey{}).(visibleModelRevision)
	if !ok || !revision.authoritative {
		return compute()
	}
	thinking, thinkingKnown := ThinkingEnabledFromContext(ctx)
	key := accountBindingMemoKey{
		model: model, finalModel: options.FinalModel,
		dispatchModel: options.MessagesDispatchModel, compact: options.Compact,
		thinking: thinking, thinkingKnown: thinkingKnown, member: member, endpointOnly: endpointOnly,
		registry: ModelRegistrySnapshotFromContext(ctx).Version(), runtime: xai.RuntimeModelMappingVersion(),
		credential: options.CredentialAccount,
	}
	// Model rewriting is independent of the endpoint. Catalog projections ask
	// about ten endpoints for each model, so share that computation across them.
	if endpointOnly {
		key.endpoint = options.Endpoint
	}
	return account.modelCatalogReadEvidence.bindings.resolve(revision.generation, key, compute)
}

func memoizedAccountBindingModel(ctx context.Context, account *Account, model string, options AccountModelBindingOptions) (string, bool) {
	value := accountBindingComputation(ctx, account, model, options, 0, false, func() accountBindingMemoValue {
		model, ok := computeAccountBindingModel(ctx, account, model, options)
		return accountBindingMemoValue{model: model, ok: ok}
	})
	return value.model, value.ok
}

func memoizedAccountBindingEndpoint(ctx context.Context, account *Account, model string, options AccountModelBindingOptions, index int, member AccountCatalogModel) bool {
	return accountBindingComputation(ctx, account, model, options, index, true, func() accountBindingMemoValue {
		return accountBindingMemoValue{ok: computeAccountBindingEndpoint(account, model, options, index, member)}
	}).ok
}
