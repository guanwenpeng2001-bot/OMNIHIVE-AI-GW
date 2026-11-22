//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorkBuddyUsageStrictNumbers(t *testing.T) {
	for _, value := range []any{nil, true, "unavailable", "", "NaN", "+Inf", math.Inf(1), math.NaN(), -1.0, json.Number("broken")} {
		_, ok := workBuddyCapacity(value)
		require.False(t, ok, "%v", value)
	}
	for _, value := range []any{0.0, "0", json.Number("0"), 12.5, "12.5"} {
		_, ok := workBuddyCapacity(value)
		require.True(t, ok)
	}
	for _, raw := range []string{
		`{}`, `{"CreditGroups":[{"CycleTotalCapacity":"unavailable","CycleUsedCapacity":"unavailable"}]}`,
		`{"CreditGroups":[{"CycleTotalCapacity":20}]}`,
		`{"Packages":[{"CycleTotalCapacity":"bad","CycleUsedCapacity":0}]}`,
		`{"Packages":[{"CycleRemainCapacity":false}]}`,
		`{"CreditGroups":[{"Kind":"plan_base","CycleTotalCapacity":"invalid","CycleUsedCapacity":0},{"Kind":"addon","CycleTotalCapacity":2,"CycleUsedCapacity":1}]}`,
	} {
		var sum map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &sum))
		require.Nil(t, parseWorkBuddyUsage(sum, nil, "cn"), raw)
	}
	require.NotNil(t, parseWorkBuddyUsage(map[string]any{"CreditGroups": []any{map[string]any{"CycleTotalCapacity": "0", "CycleUsedCapacity": "0"}}}, nil, "cn"))
}

func TestWorkBuddyUsageInvalidDoesNotOverwriteSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"CreditGroups":[{"Kind":"plan_base","CycleTotalCapacity":"unavailable","CycleUsedCapacity":"unavailable"}]}}`)
	}))
	defer server.Close()
	a := wbAccount("cn", server.URL, AccountTypeOAuth)
	old := &WorkBuddyUsageSnapshot{BaseTotal: 50, TotalRemaining: 25, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	old.bindIdentity(a)
	a.Extra = map[string]any{WorkBuddyUsageSnapshotExtraKey: old}
	repo := &runtimeSnapshotTestRepo{current: a}
	svc := &AccountUsageService{accountRepo: repo, workBuddyOAuth: NewWorkBuddyOAuthService(wbConfig(server.URL), nil)}
	got, err := svc.getWorkBuddyUsage(t.Context(), a, false)
	require.NoError(t, err)
	require.Equal(t, 25.0, got.WorkBuddy.TotalRemaining)
	_, err = svc.getWorkBuddyUsage(t.Context(), a, true)
	require.Error(t, err)
	require.Zero(t, repo.writes)
	require.Same(t, old, a.Extra[WorkBuddyUsageSnapshotExtraKey])
	for _, key := range []string{"site", "uid", "domain", "enterprise_id", "access_token", CredentialSecretVersionKey} {
		other := snapshotOAuthRefreshAccount(a)
		other.Credentials[key] = "changed"
		require.False(t, old.matchesIdentity(other), key)
		other.Credentials["api_key"] = "new-key"
		ClearAccountRuntimeObservations(other)
		require.NotContains(t, other.Extra, WorkBuddyUsageSnapshotExtraKey)
	}
}

func TestWorkBuddyUsageIdentityChangeDuringFetch(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/billing/meter/get-user-resource-summary" {
			once.Do(func() { close(started) })
			<-release
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"CreditGroups":[{"Kind":"plan_base","CycleTotalCapacity":100,"CycleUsedCapacity":20}]}}`)
		} else {
			w.WriteHeader(503)
		}
	}))
	defer server.Close()
	a := wbAccount("cn", server.URL, AccountTypeOAuth)
	repo := &runtimeSnapshotTestRepo{current: snapshotOAuthRefreshAccount(a)}
	svc := &AccountUsageService{accountRepo: repo, workBuddyOAuth: NewWorkBuddyOAuthService(wbConfig(server.URL), nil)}
	done := make(chan error, 1)
	go func() { _, err := svc.getWorkBuddyUsage(context.Background(), a, true); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("usage request did not start")
	}
	repo.current.Credentials["site"] = "global"
	repo.current.Credentials["uid"] = "other-user"
	close(release)
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrAccountRuntimeIdentityChanged)
	case <-time.After(5 * time.Second):
		t.Fatal("usage request did not finish")
	}
	require.Zero(t, repo.writes)
}
