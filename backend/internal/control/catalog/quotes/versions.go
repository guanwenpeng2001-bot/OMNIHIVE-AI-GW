package quotes

import (
	"context"
	"fmt"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

type PublishedVersions interface {
	At(context.Context, string, time.Time) ([]pricing.Version, error)
}

func PublicationScope(p catalog.Perspective, modelID string) string {
	if p.Public() {
		return fmt.Sprintf("workspace/%d/public/%s", p.Workspace, modelID)
	}
	return fmt.Sprintf("workspace/%d/project/%d/actor/%d/scope/%d/%s", p.Workspace, p.Project, p.Actor, p.AccessVersion, modelID)
}

// Estimate uses the current authorized projection to constrain all historical
// reads. The reference adapter supplies a current estimate when no exact
// publication covers that instant.
func (s Service) Estimate(ctx context.Context, p catalog.Perspective, d catalog.Detail, usage pricing.Usage, at, asOf time.Time) (pricing.Result, error) {
	if (!p.Public() && d.Model.AccessState != "allowed") || at.After(asOf) {
		return pricing.Unpriced(), nil
	}
	if s.Published != nil {
		versions, err := s.Published.At(ctx, PublicationScope(p, d.Model.ID), at)
		if err != nil {
			return pricing.Result{}, err
		}
		version, err := pricing.Resolve(pricing.Candidates{Published: versions}, at)
		if err != nil {
			return pricing.Result{}, err
		}
		if version != nil {
			return (pricing.Frozen{Version: *version, Usage: usage, At: at}).Calculate()
		}
	}
	if !at.Equal(asOf) {
		return pricing.Unpriced(), nil
	}
	v, err := Reference(d, asOf)
	if err != nil {
		return pricing.Result{}, err
	}
	return (pricing.Frozen{Version: v, Usage: usage, At: asOf}).Calculate()
}
