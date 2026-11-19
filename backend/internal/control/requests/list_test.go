package requests

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/requests"
	"github.com/stretchr/testify/require"
)

func TestRequestListCursor(t *testing.T) {
	h := New(nil, nil, "fixture-key")
	view := domain.Projection{Scope: domain.Scope{WorkspaceID: 3, ProjectID: 4}}
	r := request{actor: 7, version: 12, view: view}
	a, err := parsePage(url.Values{"key_id": {"9", "8", "9"}, "outcome": {"failed", "succeeded"}})
	require.NoError(t, err)
	b, err := parsePage(url.Values{"key_id": {"8", "9"}, "outcome": {"succeeded", "failed"}, "limit": {"10"}})
	require.NoError(t, err)
	matchA, err := cursorMatch(r, a.Filter)
	require.NoError(t, err)
	matchB, err := cursorMatch(r, b.Filter)
	require.NoError(t, err)
	require.Equal(t, matchA, matchB)

	now := time.Now().UTC()
	original := cursor{Match: matchA, AsOf: now, After: domain.Seek{
		StartedAt: now.Add(-time.Second), ID: "1af37082-64b0-4eca-93a7-4693d7f2e9a1",
	}}
	token, err := h.encodeCursor(original)
	require.NoError(t, err)
	got, err := h.decodeCursor(token, matchB, now)
	require.NoError(t, err)
	require.Equal(t, original, got)

	_, err = h.decodeCursor(token, matchB, now.Add(15*time.Minute))
	require.ErrorIs(t, err, snapshotExpired)
	_, err = h.decodeCursor(token+"X", matchB, now)
	var invalid *fault.Fault
	require.True(t, errors.As(err, &invalid))
	require.Equal(t, "/cursor", invalid.Field)

	r.view.Finance = true
	matchC, err := cursorMatch(r, b.Filter)
	require.NoError(t, err)
	require.NotEqual(t, matchA, matchC)
	_, err = h.decodeCursor(token, matchC, now)
	require.Error(t, err)
}
