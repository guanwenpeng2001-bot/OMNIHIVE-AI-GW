package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/gatewayerror"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type publicOpenAIModel struct {
	publicClientModelMetadata
	ID              string `json:"id"`
	Name            string `json:"name"`
	Object          string `json:"object"`
	Created         *int64 `json:"created,omitempty"`
	OwnedBy         string `json:"owned_by"`
	CreatedAtSource string `json:"created_at_source,omitempty"`
}
type publicAnthropicModel struct {
	publicClientModelMetadata
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	DisplayName string  `json:"display_name"`
	CreatedAt   *string `json:"created_at,omitempty"`
	MaxTokens   *int64  `json:"max_tokens,omitempty"`
}

// Additive discovery fields let clients consume known limits without knowing
// our richer catalog endpoint. Unknown or reference-only facts remain omitted.
type publicClientModelMetadata struct {
	ContextWindow            *int64    `json:"context_window,omitempty"`
	ContextLength            *int64    `json:"context_length,omitempty"`
	MaxInputTokens           *int64    `json:"max_input_tokens,omitempty"`
	MaxOutputTokens          *int64    `json:"max_output_tokens,omitempty"`
	SupportsReasoning        *bool     `json:"supports_reasoning,omitempty"`
	SupportedReasoningLevels *[]string `json:"supported_reasoning_levels,omitempty"`
	SupportsFunctionCalling  *bool     `json:"supports_function_calling,omitempty"`
	InputModalities          *[]string `json:"input_modalities,omitempty"`
	OutputModalities         *[]string `json:"output_modalities,omitempty"`
}

func clientModelMetadata(observations []service.UpstreamModelMetadata) publicClientModelMetadata {
	facts := aggregatePublicMetadata(observations)
	return publicClientModelMetadata{
		ContextWindow:            facts.TotalContext.Guaranteed,
		ContextLength:            facts.TotalContext.Guaranteed,
		MaxInputTokens:           facts.InputContext.Guaranteed,
		MaxOutputTokens:          facts.MaxOutputTokens.Guaranteed,
		SupportsReasoning:        facts.Reasoning.Guaranteed,
		SupportedReasoningLevels: facts.ReasoningLevels.Guaranteed,
		SupportsFunctionCalling:  facts.SupportsFunctionCalling.Guaranteed,
		InputModalities:          facts.InputModalities.Guaranteed,
		OutputModalities:         facts.OutputModalities.Guaranteed,
	}
}

type publicCatalogModel struct {
	ID              string              `json:"id"`
	CanonicalID     string              `json:"canonical_id"`
	Aliases         []string            `json:"aliases"`
	CreatedAt       *string             `json:"created_at"`
	CreatedAtSource string              `json:"created_at_source"`
	Capabilities    publicModelMetadata `json:"capabilities"`
	ModelVersion    publicFact[string]  `json:"model_version"`
	Endpoints       []string            `json:"endpoints"`
}
type publicGoogleModel struct {
	Name                       string   `json:"name"`
	BaseModelID                string   `json:"baseModelId"`
	Version                    string   `json:"version,omitempty"`
	DisplayName                string   `json:"displayName"`
	InputTokenLimit            *int64   `json:"inputTokenLimit,omitempty"`
	OutputTokenLimit           *int64   `json:"outputTokenLimit,omitempty"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}
type publicModelCursor struct {
	Scope string `json:"scope"`
	After string `json:"after"`
}

func publicModelHeaders(ctx *gin.Context, revision string) {
	ctx.Header("Cache-Control", "private, no-store")
	ctx.Writer.Header().Add("Vary", "Authorization")
	ctx.Writer.Header().Add("Vary", "X-API-Key")
	ctx.Writer.Header().Add("Vary", "X-Goog-Api-Key")
	ctx.Writer.Header().Add("Vary", "anthropic-version")
	if revision != "" {
		ctx.Header("X-Model-Catalog-Revision", revision)
	}
}
func (h *GatewayHandler) publicModelView(ctx *gin.Context) (service.VisibleModelSet, map[string][]service.UpstreamModelMetadata, string, bool) {
	ctx.Request = ctx.Request.WithContext(service.WithModelRegistrySnapshot(ctx.Request.Context(), service.ModelRegistrySnapshotFromContext(ctx.Request.Context())))
	if raw, exists := ctx.Get("project_key_policy"); exists {
		if policy, ok := raw.(*identity.KeyPolicy); ok {
			ctx.Request = ctx.Request.WithContext(middleware.WithProjectKeyModelAccess(ctx.Request.Context(), policy))
		}
	}
	key, _ := middleware.GetAPIKeyFromContext(ctx)
	var groupID *int64
	platform := ""
	if key != nil && key.Group != nil {
		groupID = &key.Group.ID
		platform = key.Group.Platform
		ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), ctxkey.Group, key.Group))
	}
	if forced, exists := middleware.GetForcePlatformFromContext(ctx); exists {
		platform = forced
	}
	view, metadata, err := h.gatewayService.VisibleModelsWithMetadata(ctx.Request.Context(), groupID, platform)
	publicModelHeaders(ctx, view.Revision)
	if err != nil {
		reason := "catalog_read_failed"
		switch {
		case errors.Is(err, service.ErrVisibleModelRevisionChanged):
			reason = "catalog_revision_changed"
		case errors.Is(err, context.DeadlineExceeded):
			reason = "catalog_read_timeout"
		case errors.Is(err, context.Canceled):
			reason = "catalog_read_canceled"
		}
		requestID, _ := ctx.Request.Context().Value(ctxkey.RequestID).(string)
		slog.WarnContext(ctx.Request.Context(), "public model catalog unavailable", "reason", reason, "request_id", requestID, "platform", platform)
		gatewayerror.Write(ctx, 503, "catalog_unavailable", "Model catalog unavailable")
		return view, nil, platform, false
	}

	sort.Strings(view.Models)
	return view, metadata, platform, true
}
func publicModelLimit(ctx *gin.Context, name string, defaultLimit, maxLimit int) (int, bool) {
	raw := ctx.Query(name)
	if raw == "" {
		return defaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxLimit {
		gatewayerror.Write(ctx, 400, "invalid_pagination", fmt.Sprintf("%s must be between 1 and %d", name, maxLimit))
		return 0, false
	}
	return limit, true
}
func publicCursorScope(ctx *gin.Context, view service.VisibleModelSet, source string) string {
	key, _ := middleware.GetAPIKeyFromContext(ctx)
	var keyID, groupID int64
	platform := ""
	if key != nil {
		keyID = key.ID
		if key.Group != nil {
			groupID = key.Group.ID
			platform = key.Group.Platform
		}
	}
	if forced, ok := middleware.GetForcePlatformFromContext(ctx); ok {
		platform = forced
	}
	body, _ := json.Marshal(struct {
		Key, Group                 int64
		Platform, Revision, Source string
		Models                     []string
	}{keyID, groupID, platform, view.Revision, source, view.Models})
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}
func publicCursorPage(ctx *gin.Context, view service.VisibleModelSet, source, tokenName string, limit int) ([]string, string, bool) {
	scope := publicCursorScope(ctx, view, source)
	start := 0
	if token := ctx.Query(tokenName); token != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		cursor := publicModelCursor{}
		if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Scope != scope {
			gatewayerror.Write(ctx, 400, "invalid_page_token", "Page token is invalid or the authorized catalog changed")
			return nil, "", false
		}
		index := sort.SearchStrings(view.Models, cursor.After)
		if index == len(view.Models) || view.Models[index] != cursor.After {
			gatewayerror.Write(ctx, 400, "invalid_page_token", "Page token is no longer available")
			return nil, "", false
		}
		start = index + 1
	}
	end := min(start+limit, len(view.Models))
	next := ""
	if end < len(view.Models) {
		body, _ := json.Marshal(publicModelCursor{scope, view.Models[end-1]})
		next = base64.RawURLEncoding.EncodeToString(body)
	}
	return view.Models[start:end], next, true
}
func containsPublicModel(models []string, id string) bool {
	index := sort.SearchStrings(models, id)
	return index < len(models) && models[index] == id
}

func (h *GatewayHandler) publicModels(ctx *gin.Context) {
	view, metadata, platform, ok := h.publicModelView(ctx)
	if !ok {
		return
	}
	ids := view.Models
	if id := ctx.Param("model"); id != "" {
		if !containsPublicModel(ids, id) {
			gatewayerror.Write(ctx, 404, "model_not_found", "Model is not available")
			return
		}
		ids = []string{id}
	}
	if gatewayerror.Protocol(ctx.Request) == "anthropic" {
		ids = append([]string{}, ids...)
		sort.Slice(ids, func(left, right int) bool {
			leftTime, _ := publicModelCreatedAt(metadata[ids[left]])
			rightTime, _ := publicModelCreatedAt(metadata[ids[right]])
			if leftTime == nil && rightTime != nil {
				return false
			}
			if leftTime != nil && rightTime == nil {
				return true
			}
			if leftTime != nil && rightTime != nil && !leftTime.Equal(*rightTime) {
				return leftTime.After(*rightTime)
			}
			return ids[left] < ids[right]
		})
		hasMore := false
		if ctx.Param("model") == "" {
			limit, ok := publicModelLimit(ctx, "limit", 20, 1000)
			if !ok {
				return
			}
			after, before := ctx.Query("after_id"), ctx.Query("before_id")
			if after == "" {
				after = ctx.Query("after")
			}
			if before == "" {
				before = ctx.Query("before")
			}
			if after != "" && before != "" {
				gatewayerror.Write(ctx, 400, "invalid_pagination", "Use only one of after_id or before_id")
				return
			}
			if after != "" || before != "" {
				boundary := after
				if boundary == "" {
					boundary = before
				}
				index := slices.Index(ids, boundary)
				if index < 0 {
					gatewayerror.Write(ctx, 400, "invalid_pagination", "Pagination model is not available")
					return
				}
				if after != "" {
					ids = ids[index+1:]
				} else {
					ids = ids[:index]
				}
			}
			hasMore = len(ids) > limit
			if before != "" && hasMore {
				ids = ids[len(ids)-limit:]
			} else if hasMore {
				ids = ids[:limit]
			}
		}
		entries := make([]publicAnthropicModel, 0, len(ids))
		for _, id := range ids {
			created, _ := publicModelCreatedAt(metadata[id])
			var stamp *string
			if created != nil {
				value := created.Format(time.RFC3339Nano)
				stamp = &value
			}
			clientMetadata := clientModelMetadata(metadata[id])
			entries = append(entries, publicAnthropicModel{publicClientModelMetadata: clientMetadata, ID: id, Type: "model", DisplayName: id, CreatedAt: stamp, MaxTokens: clientMetadata.MaxOutputTokens})
		}
		if ctx.Param("model") != "" {
			ctx.JSON(200, entries[0])
			return
		}
		var first, last *string
		if len(ids) > 0 {
			first = &ids[0]
			last = &ids[len(ids)-1]
		}
		ctx.JSON(200, gin.H{"data": entries, "has_more": hasMore, "first_id": first, "last_id": last})
		return
	}
	entries := make([]publicOpenAIModel, 0, len(ids))
	for _, id := range ids {
		created, source := publicModelCreatedAt(metadata[id])
		var stamp *int64
		if created != nil {
			value := created.Unix()
			stamp = &value
		}
		if created == nil {
			source = ""
		}
		entries = append(entries, publicOpenAIModel{publicClientModelMetadata: clientModelMetadata(metadata[id]), ID: id, Name: id, Object: "model", OwnedBy: platform, Created: stamp, CreatedAtSource: source})
	}
	if ctx.Param("model") != "" {
		ctx.JSON(200, entries[0])
		return
	}
	ctx.JSON(200, gin.H{"object": "list", "data": entries})
}

// ModelCatalog is the rich projection of precisely the same authorized revision.
func (h *GatewayHandler) ModelCatalog(ctx *gin.Context) {
	view, metadata, platform, ok := h.publicModelView(ctx)
	if !ok {
		return
	}
	ids := view.Models
	next := ""
	if id := ctx.Param("model"); id != "" {
		if !containsPublicModel(ids, id) {
			gatewayerror.Write(ctx, 404, "model_not_found", "Model is not available")
			return
		}
		ids = []string{id}
	} else {
		limit, valid := publicModelLimit(ctx, "limit", 100, 1000)
		if !valid {
			return
		}
		ids, next, ok = publicCursorPage(ctx, view, "catalog", "cursor", limit)
		if !ok {
			return
		}
	}
	canonical := service.ModelRegistrySnapshotFromContext(ctx.Request.Context()).LegacyCatalogCanonicalNames(platform, view.Models)
	entries := make([]publicCatalogModel, 0, len(ids))
	for _, id := range ids {
		// Upstream wire IDs are private routing targets, never canonical identities.
		aliases := []string{}
		for _, candidate := range view.Models {
			if candidate != id && canonical[candidate] == canonical[id] {
				aliases = append(aliases, candidate)
			}
		}
		created, source := publicModelCreatedAt(metadata[id])
		var stamp *string
		if created != nil {
			value := created.Format(time.RFC3339Nano)
			stamp = &value
		}
		entries = append(entries, publicCatalogModel{ID: id, CanonicalID: canonical[id], Aliases: aliases, CreatedAt: stamp, CreatedAtSource: source, Capabilities: aggregatePublicMetadata(metadata[id]), ModelVersion: publicModelVersions(metadata[id]), Endpoints: publicExecutableEndpoints(view.ExecutableEndpoints[id])})
	}
	if ctx.Param("model") != "" {
		ctx.JSON(200, gin.H{"model": entries[0], "revision": view.Revision, "fully_enumerable": view.FullyEnumerable})
		return
	}
	ctx.JSON(200, gin.H{"data": entries, "revision": view.Revision, "fully_enumerable": view.FullyEnumerable, "next_cursor": next})
}
func (h *GatewayHandler) publicGoogleModels(ctx *gin.Context) {
	view, metadata, _, ok := h.publicModelView(ctx)
	if !ok {
		return
	}
	ids := view.Models
	next := ""
	if id := ctx.Param("model"); id != "" {
		if !containsPublicModel(ids, id) {
			gatewayerror.Write(ctx, 404, "model_not_found", "Model is not available")
			return
		}
		ids = []string{id}
	} else {
		limit, valid := publicModelLimit(ctx, "pageSize", 50, 1000)
		if !valid {
			return
		}
		ids, next, ok = publicCursorPage(ctx, view, "google", "pageToken", limit)
		if !ok {
			return
		}
	}
	entries := make([]publicGoogleModel, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, publicGoogleModelEntry(view, metadata, id))
	}
	if ctx.Param("model") != "" {
		ctx.JSON(http.StatusOK, entries[0])
		return
	}
	envelope := gin.H{"models": entries}
	if next != "" {
		envelope["nextPageToken"] = next
	}
	ctx.JSON(http.StatusOK, envelope)
}

// LegacyCodexModelsRequested supports the explicit client_version migration only
// until 2027-03-21 UTC. Clients must use /backend-api/codex/models thereafter;
// ordinary /v1/models always uses the caller protocol and never guesses by UA.
func LegacyCodexModelsRequested(ctx *gin.Context) bool {
	return legacyCodexModelsRequestedAt(ctx, time.Now())
}
func legacyCodexModelsRequestedAt(ctx *gin.Context, now time.Time) bool {
	sunset := time.Date(2027, time.March, 21, 0, 0, 0, 0, time.UTC)
	if ctx.Query("client_version") == "" || !now.Before(sunset) {
		return false
	}
	ctx.Header("Deprecation", "true")
	ctx.Header("Sunset", sunset.Format(http.TimeFormat))
	ctx.Header("Link", `</backend-api/codex/models>; rel="successor-version"`)
	publicModelHeaders(ctx, "")
	return true
}

// Creation is a model fact: prefer explicit upstream evidence, then approved
// registry facts. Refresh/account timestamps never participate. An alias spanning
// several upstream models uses their earliest evidenced creation at that priority.
func publicModelCreatedAt(observations []service.UpstreamModelMetadata) (*time.Time, string) {
	var selected *time.Time
	selectedPriority := 0
	source := "unavailable"
	for _, observation := range observations {
		if observation.CreatedAtStatus != "available" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, observation.CreatedAt)
		if err != nil {
			continue
		}
		evidence := observation.FieldEvidence["created_at"]
		if evidence.Stale {
			continue
		}
		// Reject contradictory identities when present, but retain older native
		// facts without identity stamps and the resolver's reviewed registry aliases.
		if observation.ID != "" && evidence.MatchedModelID != "" &&
			observation.ID != evidence.MatchedModelID && (evidence.Source == "upstream" || evidence.MatchRule != "reviewed_alias") {
			continue
		}
		if observation.ProviderProfile != "" && evidence.ProviderProfile != "" &&
			observation.ProviderProfile != evidence.ProviderProfile {
			continue
		}
		priority := 1
		publicSource := "registry"
		switch evidence.Source {
		case "upstream":
			priority = 2
			publicSource = "upstream"
		case "platform-preset", "models.dev", "litellm", "registry":
			// Approved registry facts retain their existing public category.
		default:
			// A date without identifiable evidence is not a creation fact.
			continue
		}
		if selected == nil || priority > selectedPriority || (priority == selectedPriority && parsed.Before(*selected)) {
			selected = &parsed
			selectedPriority = priority
			source = publicSource
		}
	}
	return selected, source
}
func publicModelVersions(observations []service.UpstreamModelMetadata) publicFact[string] {
	values := make([]*string, 0, len(observations))
	for _, observation := range observations {
		var value *string
		if observation.ModelVersionStatus == "available" && observation.ModelVersion != "" {
			version := observation.ModelVersion
			value = &version
		}
		values = append(values, value)
	}
	result := aggregatePublicFact(values)
	sort.Strings(result.Available)
	return result
}
func publicExecutableEndpoints(endpoints []string) []string {
	result := []string{}
	for _, endpoint := range endpoints {
		switch endpoint {
		case "responses", "chat_completions", "messages", "gemini", "count_tokens", "embeddings", "images", "live", "alpha_search", "grok_media_generation":
			result = append(result, endpoint)
		}
	}
	sort.Strings(result)
	return result
}

func publicGoogleModelEntry(view service.VisibleModelSet, metadata map[string][]service.UpstreamModelMetadata, id string) publicGoogleModel {
	observations := []service.UpstreamModelMetadata{}
	methods := map[string]bool{}
	for index, binding := range view.Bindings[id] {
		if binding.Endpoint != service.CompositeRouteEndpointGemini || index >= len(metadata[id]) {
			continue
		}
		observation := metadata[id][index]
		observations = append(observations, observation)
		for _, method := range observation.SupportedMethods {
			switch method {
			case "generateContent", "streamGenerateContent", "countTokens":
				methods[method] = true
			}
		}
	}
	facts := aggregatePublicMetadata(observations)
	version := ""
	versions := publicModelVersions(observations)
	if versions.Guaranteed != nil {
		version = *versions.Guaranteed
	}
	actions := []string{}
	for method := range methods {
		actions = append(actions, method)
	}
	sort.Strings(actions)
	return publicGoogleModel{Name: "models/" + strings.TrimPrefix(id, "models/"), BaseModelID: strings.TrimPrefix(id, "models/"), DisplayName: id, Version: version, InputTokenLimit: facts.InputContext.Guaranteed, OutputTokenLimit: facts.MaxOutputTokens.Guaranteed, SupportedGenerationMethods: actions}
}
