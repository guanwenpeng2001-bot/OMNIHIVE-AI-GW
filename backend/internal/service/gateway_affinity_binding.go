package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
)

// AffinityBindingState records the first version observed by this request.
// Re-reads must not give an old request authority over a newer binding.
type AffinityBindingState struct{ Versions sync.Map }
type affinityBindingStateKey struct{}
type affinityAdmissionKey struct{}

func WithAffinityBindingState(ctx context.Context) context.Context {
	if AffinityBindingStateFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, affinityBindingStateKey{}, &AffinityBindingState{})
}
func AffinityBindingStateFromContext(ctx context.Context) *AffinityBindingState {
	state, _ := ctx.Value(affinityBindingStateKey{}).(*AffinityBindingState)
	return state
}
func WithAffinityAdmission(ctx context.Context) context.Context {
	return context.WithValue(ctx, affinityAdmissionKey{}, true)
}
func AffinityAdmitted(ctx context.Context) bool {
	admitted, _ := ctx.Value(affinityAdmissionKey{}).(bool)
	return admitted
}
func IsV2AffinityKey(key string) bool {
	return strings.HasPrefix(key, "v2:binding:") || strings.HasPrefix(key, "gemini:v2:binding:")
}

// GenerateScopedSessionHash is an opt-in primitive for the request plan. Callers
// must gate it with gateway.session_affinity_enabled (default false), populate
// the authenticated scope and configured HMAC pair, and pass only validated
// explicit session signals or trusted continuation mappings. An empty signal
// uses the first user anchor, never the legacy IP/UA or cache-control hash.
// The disabled path must call GenerateSessionHash and retain legacy bindings.
func (s *GatewayService) GenerateScopedSessionHash(parsed *ParsedRequest, explicit string) string {
	if parsed == nil || parsed.SessionContext == nil || parsed.SessionContext.OwnerID <= 0 || parsed.SessionContext.APIKeyID <= 0 {
		return ""
	}
	scope := parsed.SessionContext
	if strings.TrimSpace(scope.RouteScope) == "" || parsed.protocol == "" || strings.TrimSpace(parsed.Model) == "" {
		return ""
	}
	if explicit == "" {
		explicit = stableAffinityIdentity(parsed)
	}
	if explicit == "" {
		return ""
	}
	var payload bytes.Buffer
	writeAffinityField(&payload, "binding", []byte("v2"))
	writeAffinityField(&payload, "owner", []byte(strconv.FormatInt(scope.OwnerID, 10)))
	writeAffinityField(&payload, "api-key", []byte(strconv.FormatInt(scope.APIKeyID, 10)))
	writeAffinityField(&payload, "group", []byte(strconv.FormatInt(derefGroupID(parsed.GroupID), 10)))
	writeAffinityField(&payload, "route", []byte(scope.RouteScope))
	writeAffinityField(&payload, "protocol", []byte(parsed.protocol))
	writeAffinityField(&payload, "model", []byte(parsed.Model))
	writeAffinityField(&payload, "signal", []byte(explicit))
	if len(scope.AffinityHMACKey) > 0 || scope.AffinityHMACKeyVersion != "" {
		identity := hmacAffinityIdentity(payload.Bytes(), scope.AffinityHMACKey, scope.AffinityHMACKeyVersion)
		if identity == "" {
			return ""
		}
		return "v2:binding:" + strings.TrimPrefix(identity, "v2:")
	}
	sum := sha256.Sum256(payload.Bytes())
	return "v2:binding:sha256:" + hex.EncodeToString(sum[:])
}
