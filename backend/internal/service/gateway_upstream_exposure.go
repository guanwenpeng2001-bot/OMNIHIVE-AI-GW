package service

import (
	"context"
	"strings"
)

const (
	// ExposureProviderHeader reports the concrete provider platform that served
	// the request. Together with ExposureRegionHeader and the gateway trace id
	// already emitted as "request_id" it is the complete desensitized upstream
	// attribution the gateway returns to a client.
	ExposureProviderHeader = "X-Gateway-Provider"
	// ExposureRegionHeader reports the region constraint of the ordered target
	// that served the request, when one was declared.
	ExposureRegionHeader = "X-Gateway-Region"
)

// CompositeTargetAttribution is the desensitized upstream attribution of one
// ordered target: the concrete provider platform and the region constraint it
// was selected for. It never carries account identity, credentials, upstream
// secrets or internal hosts, so it is safe for client responses and for request
// logs alike.
type CompositeTargetAttribution struct {
	Provider string
	Region   string
}

// CompositeTargetAttributionOf resolves the attribution of one ordered target.
// ok=false means there is nothing whitelisted to report.
func CompositeTargetAttributionOf(target *CompositeRouteTarget) (CompositeTargetAttribution, bool) {
	if target == nil {
		return CompositeTargetAttribution{}, false
	}
	attribution := CompositeTargetAttribution{
		Provider: strings.TrimSpace(target.TargetPlatform),
		Region:   strings.TrimSpace(target.Region),
	}
	if attribution.Provider == "" && attribution.Region == "" {
		return CompositeTargetAttribution{}, false
	}
	return attribution, true
}

// CompositeExposureHeaders returns the whitelisted upstream attribution headers
// for the active ordered target on the context.
func CompositeExposureHeaders(ctx context.Context) map[string]string {
	target, ok := ActiveCompositeTargetFromContext(ctx)
	if !ok {
		return nil
	}
	return CompositeTargetExposureHeaders(&target)
}

// CompositeTargetExposureHeaders returns the whitelisted attribution headers of
// one ordered target. Only non-identifying values are ever returned: account
// ids, names, credentials and upstream secrets are never exposed.
func CompositeTargetExposureHeaders(target *CompositeRouteTarget) map[string]string {
	attribution, ok := CompositeTargetAttributionOf(target)
	if !ok {
		return nil
	}
	headers := make(map[string]string, 2)
	if attribution.Provider != "" {
		headers[ExposureProviderHeader] = attribution.Provider
	}
	if attribution.Region != "" {
		headers[ExposureRegionHeader] = attribution.Region
	}
	return headers
}

// ApplyCompositeExposureHeaders writes the whitelisted attribution headers onto
// the response. It is called before the upstream response is written so the
// values are available for streaming and buffered responses alike.
func ApplyCompositeExposureHeaders(ctx context.Context, setHeader func(key, value string)) {
	if setHeader == nil {
		return
	}
	for key, value := range CompositeExposureHeaders(ctx) {
		setHeader(key, value)
	}
}
