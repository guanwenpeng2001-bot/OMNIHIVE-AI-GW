package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type gatewayPlanCapturer interface {
	CaptureRequestPlan(context.Context, service.GatewayRequestSpec) (context.Context, *service.GatewayRequestPlan)
}

// Each adapter calls this once after parsing the inbound model, before waiting
// or forwarding. It intentionally does not move the platform failover loops.
func beginGatewayRequestPlan(c *gin.Context, capturer gatewayPlanCapturer, key *service.APIKey, model, protocol string, switches int, geminiSwitches ...int) func() {
	spec := service.GatewayRequestSpec{UserID: key.UserID, APIKeyID: key.ID, Route: c.Request.URL.Path, Protocol: protocol, Model: model, MaxAccountSwitches: switches}
	if len(geminiSwitches) > 0 {
		spec.MaxAccountSwitchesGemini = geminiSwitches[0]
	}
	if key.GroupID != nil {
		spec.BillingGroupID = *key.GroupID
	}
	ctx, plan := capturer.CaptureRequestPlan(c.Request.Context(), spec)
	c.Request = c.Request.WithContext(ctx)
	stop := context.AfterFunc(ctx, plan.Cancel)
	return func() {
		stop()
		if ctx.Err() != nil {
			plan.Cancel()
		}
		plan.Finalize(c.Writer.Written())
	}
}

func claimGatewayUsage(parent context.Context) bool {
	plan := service.GatewayRequestPlanFromContext(parent)
	return plan == nil || plan.Record()
}
