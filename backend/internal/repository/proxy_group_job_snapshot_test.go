//go:build unit

package repository

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

type egressJobRow []byte

func (r egressJobRow) Scan(values ...any) error {
	*(values[0].(*[]byte)) = append([]byte(nil), r...)
	return nil
}

func TestProxyGroupJobSnapshotPrivatePersistenceRoundTrip(t *testing.T) {
	billing := service.UpstreamJobBilling{Model: "fixture", Egress: &service.ProxyGroupJobEgress{ProxyGroupID: 7, ProxyID: 11, Source: service.EgressSourceGroupBinding}}
	snapshot, err := marshalUpstreamJobBilling(billing)
	require.NoError(t, err)
	require.Contains(t, string(snapshot), `"egress":{"proxy_group_id":7`)
	raw, err := json.Marshal(map[string]any{"id": 1, "account_id": 91, "billing_snapshot": json.RawMessage(snapshot)})
	require.NoError(t, err)
	job, err := scanUpstreamJob(egressJobRow(raw))
	require.NoError(t, err)
	require.Equal(t, billing.Egress, job.Billing.Egress)
	public, err := json.Marshal(job)
	require.NoError(t, err)
	require.NotContains(t, string(public), "egress")
	require.NotContains(t, string(public), "proxy_group_id")
	batch, err := json.Marshal(&service.BatchImageJob{EgressSnapshot: billing.Egress})
	require.NoError(t, err)
	require.NotContains(t, string(batch), "Egress")
	require.NotContains(t, string(batch), "proxy_group_id")
}
