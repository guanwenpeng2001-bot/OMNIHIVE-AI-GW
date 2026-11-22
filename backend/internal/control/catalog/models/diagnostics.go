package models

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

type AdminModelDiagnostics struct {
	Model          catalog.Summary           `json:"model"`
	Facts          []catalog.ModelFacts      `json:"field_evidence"`
	Offers         []catalog.OfferDiagnostic `json:"offers"`
	CatalogVersion string                    `json:"catalog_version"`
	EvaluatedAt    time.Time                 `json:"evaluated_at"`
}

func (h *Handler) diagnostics(ctx context.Context, p catalog.Perspective, c *gin.Context) (any, error) {
	if p.Public() || !p.EndpointMetadata {
		return nil, fault.Forbidden
	}
	if _, err := h.access.Evaluate(ctx, p.Actor, p.Workspace, p.Project, "connections.read"); err != nil {
		return nil, err
	}
	id := c.Param("model_id")
	if !isIdentifier(id, "m_", 32) {
		return nil, fault.NotFound
	}
	snapshot, err := h.reader.Snapshot(ctx, p, time.Time{})
	if err != nil {
		return nil, err
	}
	for _, d := range snapshot.Details {
		if d.Model.ID == id {
			if d.Offers == nil {
				d.Offers = []catalog.OfferDiagnostic{}
			}
			return AdminModelDiagnostics{Model: d.Model, Facts: d.Facts, Offers: d.Offers, CatalogVersion: snapshot.Version, EvaluatedAt: snapshot.AsOf}, nil
		}
	}
	return nil, fault.NotFound
}
