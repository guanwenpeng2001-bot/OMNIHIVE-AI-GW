//go:build unit

package domain

import (
	"sort"
	"sync"
)

var compatibleProviderTestMu sync.Mutex

// SetCompatibleProvidersForTest installs an isolated, validated snapshot only in
// unit builds. Invalid rows panic before installation. Callers must defer restore
// (or t.Cleanup it), must not nest installations, and must not use t.Parallel for
// tests that depend on global registry contents. Writers serialize until restore;
// readers see complete immutable snapshots, including during concurrent restore.
// Restoration is idempotent. Test inputs and returned rows never alias authority.
func SetCompatibleProvidersForTest(rows []CompatibleProvider) (restore func()) {
	if err := ValidateCompatibleProviders(rows); err != nil {
		panic(err)
	}
	copied := make([]CompatibleProvider, len(rows))
	for index, provider := range rows {
		copied[index] = copyCompatibleProvider(provider)
	}
	sort.Slice(copied, func(left, right int) bool { return copied[left].ID < copied[right].ID })
	compatibleProviderTestMu.Lock()
	previous := compatibleProviders.Swap(&compatibleRegistry{rows: copied})
	var once sync.Once
	return func() { once.Do(func() { compatibleProviders.Store(previous); compatibleProviderTestMu.Unlock() }) }
}
