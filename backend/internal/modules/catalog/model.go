package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

type Perspective struct {
	Actor, Workspace, Project, AccessVersion int64
	EndpointMetadata                         bool
	Kind                                     PerspectiveKind
	KeyID                                    int64
}

type PerspectiveKind string

const (
	PerspectivePublic      PerspectiveKind = "public"
	PerspectiveProject     PerspectiveKind = "project"
	PerspectiveSelectedKey PerspectiveKind = "selected_key"
	PerspectiveAdmin       PerspectiveKind = "platform_admin"
)

func (p Perspective) Public() bool {
	return p.Kind == PerspectivePublic || p.Kind == "" && p.Project == 0
}

type publicViewerAuthKey struct{}

func WithPublicViewerAuthenticated(ctx context.Context) context.Context {
	return context.WithValue(ctx, publicViewerAuthKey{}, true)
}

func PublicViewerAuthenticated(ctx context.Context) bool {
	ok, _ := ctx.Value(publicViewerAuthKey{}).(bool)
	return ok
}

type EffectiveScope struct {
	Workspace int64 `json:"workspace_id,string"`
	Project   int64 `json:"project_id,string"`
	Version   int64 `json:"scope_version,string"`
}

type Evidence struct {
	Source     string     `json:"source"`
	Maturity   string     `json:"maturity"`
	ObservedAt *time.Time `json:"observed_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	State      string     `json:"state"`
}

func Observation(source, maturity string, observed, expires time.Time, now time.Time) Evidence {
	e := Evidence{Source: source, Maturity: maturity, State: "unknown"}
	if !observed.IsZero() && !observed.After(now) {
		observed = observed.UTC()
		e.ObservedAt, e.State = &observed, "fresh"
		if !expires.IsZero() {
			expires = expires.UTC()
			e.ExpiresAt = &expires
			if !now.Before(expires) {
				e.State = "stale"
			}
		}
	}
	return e
}

type Facts struct {
	Context             *int64   `json:"context_tokens,string"`
	ContextKind         string   `json:"context_kind"`
	Output              *int64   `json:"max_output_tokens,string"`
	EmbeddingDimensions *int64   `json:"embedding_dimensions,string"`
	Tools               *bool    `json:"tools"`
	Vision              *bool    `json:"vision"`
	Reasoning           *bool    `json:"reasoning"`
	Cache               *bool    `json:"prompt_caching"`
	PDF                 *bool    `json:"pdf"`
	Modalities          []string `json:"modalities"`
	InputModalities     []string `json:"-"`
	OutputModalities    []string `json:"-"`
	SupportedMethods    []string `json:"-"`
}

type ModelFacts struct {
	Facts
	Evidence
	Origin       Provenance            `json:"-"`
	FieldOrigins map[string]Provenance `json:"-"`
	Suppressed   []string              `json:"-"`
}

type RegistryModel struct {
	Provider, Model, Canonical, DisplayName string
	Facts                                   ModelFacts
	Capabilities                            []Capability
	Actions                                 []string
	Methods                                 []string
	Aliases                                 []string
}

type Capability struct {
	VerifiedExtensionActions []string   `json:"verified_extension_actions,omitempty"`
	Action                   string     `json:"action"`
	Protocol                 string     `json:"protocol"`
	Mode                     string     `json:"mode"`
	Tools                    string     `json:"tools"`
	Stream                   string     `json:"stream"`
	VerifiedAt               *time.Time `json:"verified_at"`
	AdapterVersion           *string    `json:"adapter_version"`
	Limitations              []string   `json:"limitations"`
	Evidence
}

type Endpoint struct {
	ID              string       `json:"id"`
	Provider        string       `json:"provider"`
	Regions         []string     `json:"regions"`
	Training        string       `json:"training"`
	Retention       string       `json:"content_retention"`
	SafetyDays      *int64       `json:"safety_retention_days"`
	RegionEvidence  *Evidence    `json:"region_evidence"`
	PrivacyEvidence *Evidence    `json:"privacy_evidence"`
	Capabilities    []Capability `json:"capabilities"`
}

type Offer struct {
	GroupID            int64
	ChannelID          int64
	Access, Eligible   bool
	EligibilityUnknown bool
	CatalogOnly        bool
	Published          bool
	Candidate          bool
	Discovered         bool
	PublicationBasis   string
	Bindings           []ModelBinding
	BindingFacts       []BindingFacts
	Actions            []string
	Facts              []ModelFacts
	Endpoints          []Endpoint
	Capabilities       []Capability
	Price              Price
}

type Candidate struct {
	Provider, Model string
	Offers          []Offer
	Canonicals      []string
}

type CatalogEntry = Candidate

// ModelBinding is internal to the read model. Use the appropriate redacted
// projection before writing any binding data to a response.
type ModelBinding struct {
	Identity                                           ModelIdentity
	AccountID, CredentialOwnerID, CredentialRevision   int64
	IdentityRevision, SnapshotRevision, RulesRevision  int64
	Profile, WireModel, Region, Action, Protocol, Mode string
	AccountLabel, AccountType                          string
	ChannelID, OfferGroupID, BillingGroupID            int64
	BillingIdentity                                    ModelIdentity
	BillingModelSource                                 string
	Eligible                                           bool
	DiscoveryState                                     string
	ObservedAt                                         *time.Time
}

type BindingFacts struct {
	Profile    string
	Facts      []ModelFacts
	Historical bool
}

type Input struct {
	RegistryVersion string
	Registry        []RegistryModel
	Models          []Candidate
	Metrics         map[string]Metrics
	Compatibility   map[string][]Compatibility
	Revision        string
}

type Window struct{ Start, End time.Time }

type Metrics struct {
	Start        time.Time `json:"window_start"`
	End          time.Time `json:"window_end"`
	Source       string    `json:"source"`
	Requests     *int64    `json:"requests,string"`
	TTFTSamples  *int64    `json:"ttft_samples,string"`
	SpeedSamples *int64    `json:"speed_samples,string"`
	SuccessRate  *float64  `json:"success_rate"`
	TTFT         *int64    `json:"ttft_p50_ms"`
	Speed        *float64  `json:"output_tokens_per_second"`
	State        string    `json:"state"`
}

type RateBand struct {
	Rate      money.Rate `json:"rate"`
	Kind      string     `json:"kind"`
	MinTokens *int64     `json:"min_tokens,string"`
	MaxTokens *int64     `json:"max_tokens,string"`
	Label     string     `json:"label,omitempty"`
}

type TimeBand struct {
	Timezone     string       `json:"timezone"`
	WeekdaysOnly bool         `json:"weekdays_only"`
	Start        string       `json:"start_time"`
	End          string       `json:"end_time"`
	Multiplier   money.Amount `json:"multiplier"`
}

type Price struct {
	State            string      `json:"state"`
	Source           string      `json:"source"`
	Input            *money.Rate `json:"input_rate"`
	Output           *money.Rate `json:"output_rate"`
	Additional       []RateBand  `json:"additional_rates"`
	ContextBasis     string      `json:"context_basis,omitempty"`
	TimeBands        []TimeBand  `json:"time_bands"`
	Reason           string      `json:"-"`
	Version          *string     `json:"-"`
	ValidFrom        *time.Time  `json:"-"`
	ValidUntil       *time.Time  `json:"-"`
	Semantics        string      `json:"-"`
	LegacyEquivalent bool        `json:"-"`
}

type Summary struct {
	ID                string          `json:"id"`
	CanonicalID       string          `json:"canonical_id,omitempty"`
	Source            *SourceAxis     `json:"source,omitempty"`
	Availability      *Availability   `json:"availability,omitempty"`
	Billing           *Billing        `json:"billing,omitempty"`
	Model             string          `json:"model"`
	DisplayName       string          `json:"display_name"`
	Provider          string          `json:"provider"`
	Modalities        []string        `json:"modalities"`
	Protocols         []string        `json:"protocols"`
	Actions           []string        `json:"actions"`
	Regions           []string        `json:"regions"`
	Context           *int64          `json:"context_tokens,string"`
	Maturity          string          `json:"maturity"`
	Version           string          `json:"catalog_version"`
	CatalogState      string          `json:"catalog_state"`
	AvailabilityState string          `json:"availability_state,omitempty"`
	AccessState       string          `json:"access_state"`
	RouteState        string          `json:"route_state"`
	PriceState        string          `json:"price_state"`
	Input             *money.Rate     `json:"input_rate"`
	Output            *money.Rate     `json:"output_rate"`
	ObservedAt        *time.Time      `json:"observed_at"`
	EvidenceState     string          `json:"evidence_state"`
	Scope             *EffectiveScope `json:"effective_scope,omitempty"`
}

type Benchmark struct {
	ID         string    `json:"benchmark_id"`
	Value      string    `json:"value"`
	Unit       string    `json:"unit"`
	Source     string    `json:"source"`
	VerifiedAt time.Time `json:"verified_at"`
}

type Detail struct {
	Provenance       []Provenance    `json:"provenance"`
	CommonFacts      *Facts          `json:"common_facts,omitempty"`
	ScopeVersion     *int64          `json:"scope_version,string,omitempty"`
	Offers           []Offer         `json:"-"`
	ReferencePrice   *Price          `json:"reference_price,omitempty"`
	EligibleGroups   []int64         `json:"-"`
	Model            Summary         `json:"model"`
	Aliases          []string        `json:"aliases"`
	Facts            []ModelFacts    `json:"model_facts"`
	Capabilities     []Capability    `json:"capabilities"`
	Endpoints        []Endpoint      `json:"endpoints"`
	PriceVersion     *string         `json:"price_version"`
	PriceSource      string          `json:"price_source"`
	AdditionalRates  []RateBand      `json:"additional_rates"`
	ContextBasis     string          `json:"context_basis,omitempty"`
	TimeBands        []TimeBand      `json:"time_bands"`
	Metrics          Metrics         `json:"metrics"`
	Benchmarks       []Benchmark     `json:"benchmarks"`
	Compatibility    []Compatibility `json:"compatibility,omitempty"`
	DocumentationURL *string         `json:"documentation_url"`
	Redactions       []string        `json:"redactions"`
}

type Snapshot struct {
	Version      string    `json:"catalog_version"`
	AsOf         time.Time `json:"as_of"`
	Details      []Detail  `json:"models"`
	ScopeVersion *int64    `json:"scope_version,string,omitempty"`
}

type Acquisition interface {
	Load(context.Context, Perspective, Window, time.Time) (Input, error)
}

type CatalogPublisher interface {
	Publish(context.Context, Snapshot, string, time.Time) error
}

type CatalogReader interface {
	Snapshot(context.Context, Perspective, time.Time) (Snapshot, error)
}

func StableID(provider, model string) string { return "m_" + Digest([2]string{provider, model})[:32] }

func Digest(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func GatewayAction(method string) (action, protocol, adapterAction string) {
	switch method {
	case "chat_completions":
		return "text.chat", "openai_chat", "chat"
	case "responses":
		return "text.chat", "openai_responses", "responses"
	case "messages":
		return "text.chat", "anthropic_messages", "messages"
	case "gemini", "generateContent":
		return "text.chat", "gemini_generate", "generate_content"
	case "count_tokens":
		return "text.count_tokens", "anthropic_messages", "count_tokens"
	case "embeddings":
		return "text.embeddings", "openai_embeddings", "embeddings"
	case "images", "images/generations":
		return "image.generate", "openai_images", "image.generate"
	case "images/edits":
		return "image.edit", "openai_images", "image.edit"
	case "rerank":
		return "text.rerank", "cohere_rerank", "rerank"
	case "speech":
		return "audio.tts", "openai_audio", "audio.tts"
	case "transcriptions":
		return "audio.stt", "openai_audio", "audio.stt"
	case "live":
		return "realtime", "gemini_live", "live"
	case "alpha_search":
		return "search", "grok_native", "alpha_search"
	case "grok_media_generation":
		return "image.generate", "grok_native", "image.generate"
	default:
		return "", "", ""
	}
}

func Support(v *bool) string {
	if v == nil {
		return "unknown"
	}
	if *v {
		return "supported"
	}
	return "unsupported"
}
