//go:build unit

package admin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeInt64IDList(t *testing.T) {
	tests := []struct {
		name string
		in   []int64
		want []int64
	}{
		{"nil input", nil, nil},
		{"empty input", []int64{}, nil},
		{"single element", []int64{5}, []int64{5}},
		{"already sorted unique", []int64{1, 2, 3}, []int64{1, 2, 3}},
		{"duplicates removed", []int64{3, 1, 3, 2, 1}, []int64{1, 2, 3}},
		{"zero filtered", []int64{0, 1, 2}, []int64{1, 2}},
		{"negative filtered", []int64{-5, -1, 3}, []int64{3}},
		{"all invalid", []int64{0, -1, -2}, []int64{}},
		{"sorted output", []int64{9, 3, 7, 1}, []int64{1, 3, 7, 9}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeInt64IDList(tc.in)
			if tc.want == nil {
				require.Nil(t, got)
			} else {
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestBuildAccountTodayStatsBatchCacheKey(t *testing.T) {
	tests := []struct {
		name string
		ids  []int64
		want string
	}{
		{"empty", nil, `"account_ids":null`},
		{"single", []int64{42}, `"account_ids":[42]`},
		{"multiple", []int64{1, 2, 3}, `"account_ids":[1,2,3]`},
		{"reordered duplicates", []int64{3, 1, 2, 3}, `"account_ids":[1,2,3]`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dayStart := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
			got := buildAccountTodayStatsBatchCacheKey(tc.ids, dayStart, "coverage")
			require.Contains(t, got, tc.want)
			require.Contains(t, got, `"day_start":"2026-09-19T00:00:00Z"`)
			require.Contains(t, got, `"timezone":"UTC"`)
			require.Contains(t, got, `"revision":"coverage"`)
			require.Equal(t, buildAccountTodayStatsBatchCacheKey(normalizeInt64IDList(tc.ids), dayStart, "coverage"), got)
		})
	}
}
