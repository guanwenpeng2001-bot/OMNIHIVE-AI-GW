package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/stretchr/testify/require"
)

type keyGroupCatalog struct{ details []catalog.Detail }

func (r keyGroupCatalog) Snapshot(context.Context, catalog.Perspective, time.Time) (catalog.Snapshot, error) {
	return catalog.Snapshot{Details: r.details}, nil
}

func TestCatalogKeyGroupsRestrictsAutomaticSelection(t *testing.T) {
	first, second, denied := catalog.StableID("openai", "a"), catalog.StableID("openai", "b"), catalog.StableID("openai", "c")
	store := NewWithCatalog(nil, keyGroupCatalog{[]catalog.Detail{
		{Model: catalog.Summary{ID: first, Model: "a"}, EligibleGroups: []int64{3, 7}},
		{Model: catalog.Summary{ID: second, Model: "b"}, EligibleGroups: []int64{7}},
		{Model: catalog.Summary{ID: denied, Model: "c"}},
	}})
	for _, tc := range []struct {
		name string
		ids  []string
		want []int64
	}{
		{"single", []string{second}, []int64{7}},
		{"intersection", []string{first, second}, []int64{7}},
		{"mixed native and catalog identifiers", []string{"a", second}, []int64{7}},
		{"ambiguous", []string{first}, []int64{3, 7}},
		{"unavailable", []string{second, denied}, []int64{}},
		{"unknown", []string{second, "unknown"}, []int64{}},
		{"legacy issuance", []string{"b"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group, err := store.catalogKeyGroups(context.Background(), identity.Access{}, identity.ModelAccess{Mode: "allow_list", IDs: tc.ids})
			require.NoError(t, err)
			require.Equal(t, tc.want, group)
		})
	}
}
