package service

import (
	"net/url"
	"reflect"
	"strings"
	"sync"
)

const accountBaseURLCapacity = 8

// Account is copied by value throughout scheduling. Keep the lock outside it;
// immutable entries can be shared by those copies without copying a mutex.
var accountBaseURLMu sync.RWMutex

type accountBaseURLIdentity struct {
	id, workspace, revision                   int64
	credentials                               uintptr
	credentialRevision, platform, accountType string
}

type accountBaseURLMemo struct {
	identity accountBaseURLIdentity
	entries  map[string]accountBaseURLResult
}

type accountBaseURLResult struct {
	parsed *url.URL
	err    error
}

// Only parses base URLs, never credentials, policy or DNS/SSRF decisions. Return
// a copy because endpoint builders mutate Path, RawPath, Query and Fragment.
func (account *Account) parsedBaseURL(raw string) (*url.URL, error) {
	return account.parseBaseURL(raw, url.Parse)
}

func (account *Account) parseBaseURL(raw string, parse func(string) (*url.URL, error)) (*url.URL, error) {
	if account == nil {
		return parse(raw)
	}
	identity := accountBaseURLIdentity{
		id: account.ID, workspace: account.WorkspaceID, revision: account.ModelCatalogState().IdentityRevision,
		credentials: reflect.ValueOf(account.Credentials).Pointer(), credentialRevision: account.CredentialRevision,
		platform: account.Platform, accountType: account.Type,
	}
	accountBaseURLMu.RLock()
	memo := account.baseURLMemo
	var result accountBaseURLResult
	var found bool
	if memo != nil && memo.identity == identity {
		result, found = memo.entries[raw]
	}
	accountBaseURLMu.RUnlock()
	if !found {
		accountBaseURLMu.Lock()
		memo = account.baseURLMemo
		if memo == nil || memo.identity != identity {
			memo = &accountBaseURLMemo{identity: identity, entries: make(map[string]accountBaseURLResult)}
			account.baseURLMemo = memo
		}
		result, found = memo.entries[raw]
		if !found {
			result.parsed, result.err = parse(raw)
			if len(memo.entries) >= accountBaseURLCapacity {
				clear(memo.entries)
			}
			memo.entries[raw] = result
		}
		accountBaseURLMu.Unlock()
	}
	if result.parsed == nil {
		return nil, result.err
	}
	copyURL := *result.parsed
	return &copyURL, result.err
}

func (account *Account) modelRegistryHostname(raw string) string {
	parsed, err := account.parsedBaseURL(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(parsed.Hostname())), ".")
}

func (account *Account) normalizeModelRegistryBaseURL(raw string) string {
	parsed, err := account.parsedBaseURL(strings.TrimSpace(raw))
	return normalizeParsedModelRegistryBaseURL(parsed, err)
}
