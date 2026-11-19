package byok

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLifecycleCursorBindsTenantPrincipalAndSnapshot(t *testing.T) {
	s := &Store{signing: []byte("fixture")}
	target := Target{Workspace: 1, Project: 2, ID: 3, Actor: 4}
	p := eventSeek{After: 5, AsOf: time.Now().UTC()}
	token, err := s.eventCursor(target, p)
	require.NoError(t, err)
	again, err := s.eventPage(target, token)
	require.NoError(t, err)
	require.Equal(t, p, again)
	for _, wrong := range []Target{{Workspace: 2, Project: 2, ID: 3, Actor: 4}, {Workspace: 1, Project: 5, ID: 3, Actor: 4}, {Workspace: 1, Project: 2, ID: 5, Actor: 4}, {Workspace: 1, Project: 2, ID: 3, Actor: 5}} {
		_, err = s.eventPage(wrong, token)
		require.Error(t, err)
	}
	p.AsOf = p.AsOf.Add(-time.Hour)
	token, err = s.eventCursor(target, p)
	require.NoError(t, err)
	_, err = s.eventPage(target, token)
	require.Error(t, err)
}

func TestPilotOperationSnapshotAfterUpgrade(t *testing.T) {
	raw := json.RawMessage(`{"state":"succeeded","result":{"id":"3","owner_workspace_id":"1","provider":"openai","version":"1","scope":{"member_ids":["4"]}}}`)
	completed, err := completePilotResponse(raw)
	require.NoError(t, err)
	var op struct {
		State  string
		Result Credential
	}
	require.NoError(t, json.Unmarshal(completed, &op))
	require.Equal(t, "succeeded", op.State)
	require.Equal(t, "api_key", op.Result.Auth)
	require.Equal(t, []string{"4"}, op.Result.Scope.Members)
	require.Equal(t, []string{}, op.Result.Scope.ServiceAccounts)
}
