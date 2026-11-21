package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
)

type nativeRequestLayerKey struct{}

// WithGatewayRoutingHeaders freezes client restrictions for HTTP requests,
// WebSocket turns and detached generation jobs inheriting the ingress context.
func WithGatewayRoutingHeaders(ctx context.Context, headers http.Header) (context.Context, error) {
	regionValues := headers.Values("X-OmniHive-Regions")
	retention := headers.Values("X-OmniHive-Retention")
	training := headers.Values("X-OmniHive-Training")
	if len(regionValues)+len(retention)+len(training) == 0 {
		return ctx, nil
	}
	c := policy.Constraints{}
	if len(regionValues) > 0 {
		var regions []string
		for _, line := range regionValues {
			for _, v := range strings.Split(line, ",") {
				regions = append(regions, strings.TrimSpace(v))
			}
		}
		c.Regions = &policy.List{Mode: "allow_list", Values: regions}
	}
	if len(retention) > 0 {
		if len(retention) != 1 || retention[0] != "strict_zero" {
			return ctx, fmt.Errorf("X-OmniHive-Retention must be strict_zero")
		}
		c.Retention = retention[0]
	}
	if len(training) > 0 {
		if len(training) != 1 || training[0] != "prohibited" {
			return ctx, fmt.Errorf("X-OmniHive-Training must be prohibited")
		}
		c.Training = training[0]
	}
	d := policy.Definition{Name: "request", Scope: policy.Scope{Type: "request", ID: "request"}, Constraints: c}
	if err := d.Validate(0); err != nil {
		return ctx, fmt.Errorf("invalid X-OmniHive-Regions allow list")
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return ctx, err
	}
	layer := policy.Layer{Version: fmt.Sprintf("request_%x", sha256.Sum256(payload)), Definition: d}
	return context.WithValue(ctx, nativeRequestLayerKey{}, &layer), nil
}

func GatewayRequestRoutingLayer(ctx context.Context) *policy.Layer {
	layer, _ := ctx.Value(nativeRequestLayerKey{}).(*policy.Layer)
	return layer
}

func CheckGatewayRoutingPolicy(ctx context.Context, cfg *config.Config, model string, deferredWebSocket bool) (context.Context, error) {
	// Responses WS selects its model per frame. Its scheduler checks each turn.
	if deferredWebSocket && model == "" {
		_, scoped := scope.From(ctx)
		if _, ready := nativePolicyStores.Load(cfg); scoped && ready {
			return ctx, nil
		}
		return ctx, policy.NativePolicyDenied
	}
	return withNativePolicyAccounts(ctx, cfg, model)
}

func HasGatewayRoutingConstraints(ctx context.Context) bool {
	return ctx.Value(nativeRequestLayerKey{}) != nil
}
