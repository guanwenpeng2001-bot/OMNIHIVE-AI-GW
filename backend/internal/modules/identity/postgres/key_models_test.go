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

func TestDefaultKeyGroupRequiresUniqueCommonOffer(t *testing.T) {
	first, second, denied := catalog.StableID("openai", "a"), catalog.StableID("openai", "b"), catalog.StableID("openai", "c")
	store := NewWithCatalog(nil, keyGroupCatalog{[]catalog.Detail{
		{Model: catalog.Summary{ID: first, Model: "a"}, EligibleGroups: []int64{3, 7}},
		{Model: catalog.Summary{ID: second, Model: "b"}, EligibleGroups: []int64{7}},
		{Model: catalog.Summary{ID: denied, Model: "c"}},
	}})
	for _, tc := range []struct {
		name string
		ids  []string
		want int64
	}{
		{"single", []string{second}, 7},
		{"intersection", []string{first, second}, 7},
		{"mixed native and catalog identifiers", []string{"a", second}, 7},
		{"ambiguous", []string{first}, 0},
		{"unavailable", []string{second, denied}, 0},
		{"unknown", []string{second, "unknown"}, 0},
		{"legacy issuance", []string{"b"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group, err := store.defaultKeyGroup(context.Background(), identity.Access{}, identity.ModelAccess{Mode: "allow_list", IDs: tc.ids})
			require.NoError(t, err)
			if tc.want == 0 {
				require.Nil(t, group)
			} else {
				require.Equal(t, &tc.want, group)
			}
		})
	}
}
