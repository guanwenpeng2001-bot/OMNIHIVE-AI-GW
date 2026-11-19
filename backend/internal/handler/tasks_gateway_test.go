package handler

import (
	"context"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/stretchr/testify/require"
)

type taskCatalog struct{ model catalog.Detail }

func (c taskCatalog) Snapshot(context.Context, catalog.Perspective, time.Time) (catalog.Snapshot, error) {
	return catalog.Snapshot{Version: "catalog-v", Details: []catalog.Detail{c.model}}, nil
}

func TestImageTaskCenterChecksVerifiedDispatchAccount(t *testing.T) {
	stamp := time.Now()
	a := tasks.Scope{WorkspaceID: 10, ProjectID: 11}
	ep := catalog.Endpoint{ID: "ep_" + catalog.Digest([2]int64{a.ProjectID, 12})[:32], Capabilities: []catalog.Capability{{Action: "image.generate", Protocol: "openai_images", VerifiedAt: &stamp, Evidence: catalog.Evidence{State: "fresh"}}}}
	model := catalog.Detail{Model: catalog.Summary{ID: "entry", Model: "provider-image", AccessState: "allowed", RouteState: "eligible"}, Endpoints: []catalog.Endpoint{ep}}
	gateway := ImageTaskGateway{Catalog: taskCatalog{model}}
	name, v, err := gateway.verified(t.Context(), a, "entry", 12)
	require.NoError(t, err)
	require.Equal(t, "provider-image", name)
	require.Equal(t, "catalog-v", v)
	_, _, err = gateway.verified(t.Context(), a, "entry", 13)
	require.Error(t, err)
	model.Endpoints[0].Capabilities[0].State = "stale"
	_, _, err = gateway.verified(t.Context(), a, "entry", 12)
	require.Error(t, err)
}
