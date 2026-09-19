package domain

// EndpointSupport describes a static path; the zero value rejects.
type EndpointSupport string

const (
	EndpointRejected EndpointSupport = ""
	EndpointNative   EndpointSupport = "native"
	EndpointBridge   EndpointSupport = "bridge"
)

// PlatformDescriptor contains shared static facts, not model routing, transport,
// credentials, or measured quota. Composite belongs to groups, never this table.
// A capability is an eligibility bound: account settings and specialized
// services still decide whether a particular request can actually execute.
type PlatformDescriptor struct {
	// ResponsesPOST describes an available protocol path; multi-protocol accounts resolve
	// their actual native protocol in the existing account/model services.
	ResponsesPOST                EndpointSupport `json:"responsesPost"`
	ResponsesWS                  EndpointSupport `json:"responsesWS"`
	ResponsesCompact             bool            `json:"responsesCompact"`
	NativeMultiProtocolResponses bool            `json:"nativeMultiProtocolResponses"`

	ChatOnly            bool   `json:"chatOnly"`
	ID                  string `json:"id"`
	OAuth               bool   `json:"oauth"`
	CNProvider          bool   `json:"cnProvider"`
	MultiProtocol       bool   `json:"multiProtocol"`
	SchedulingThreshold bool   `json:"schedulingThreshold"`
	ActiveProbe         bool   `json:"activeProbe"`
	QuotaMonitor        bool   `json:"quotaMonitor"`
	PassiveMonitor      bool   `json:"passiveMonitor"`
	ModelCatalog        bool   `json:"modelCatalog"`
}

var platformDescriptors = []PlatformDescriptor{
	{ID: PlatformMistral, ChatOnly: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformHunyuan, ChatOnly: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformQianfan, ChatOnly: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformVolcengine, ChatOnly: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformAnthropic, ResponsesPOST: EndpointBridge, OAuth: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformGemini, OAuth: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformOpenAI, ResponsesCompact: true, ResponsesWS: EndpointNative, ResponsesPOST: EndpointNative, OAuth: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformAntigravity, ResponsesPOST: EndpointBridge, OAuth: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformGrok, ResponsesWS: EndpointBridge, ResponsesPOST: EndpointNative, OAuth: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformKimi, NativeMultiProtocolResponses: true, ResponsesPOST: EndpointNative, CNProvider: true, MultiProtocol: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformZhipu, ResponsesPOST: EndpointBridge, CNProvider: true, MultiProtocol: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformDeepseek, NativeMultiProtocolResponses: true, ResponsesPOST: EndpointNative, CNProvider: true, MultiProtocol: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformMiniMax, NativeMultiProtocolResponses: true, ResponsesPOST: EndpointNative, CNProvider: true, MultiProtocol: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformOpenCodeGo, NativeMultiProtocolResponses: true, ResponsesPOST: EndpointNative, MultiProtocol: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformCursor, ResponsesPOST: EndpointBridge, OAuth: true, SchedulingThreshold: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformQianwen, NativeMultiProtocolResponses: true, ResponsesPOST: EndpointNative, CNProvider: true, MultiProtocol: true, ActiveProbe: true, PassiveMonitor: true, ModelCatalog: true},
	{ID: PlatformCommandCode, ResponsesPOST: EndpointBridge, MultiProtocol: true, SchedulingThreshold: true, ActiveProbe: true, QuotaMonitor: true, PassiveMonitor: true, ModelCatalog: true},
}

// PlatformDescriptors returns a copy so callers cannot mutate the authority.
func PlatformDescriptors() []PlatformDescriptor {
	return append([]PlatformDescriptor(nil), platformDescriptors...)
}

func LookupPlatform(id string) (PlatformDescriptor, bool) {
	for _, descriptor := range platformDescriptors {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return PlatformDescriptor{}, false
}

func ConcretePlatforms() []string {
	return PlatformsWith(func(PlatformDescriptor) bool { return true })
}

func PlatformsWith(include func(PlatformDescriptor) bool) []string {
	platforms := make([]string, 0, len(platformDescriptors))
	for _, descriptor := range platformDescriptors {
		if include(descriptor) {
			platforms = append(platforms, descriptor.ID)
		}
	}
	return platforms
}

func IsConcretePlatform(id string) bool {
	_, ok := LookupPlatform(id)
	return ok
}

func IsGroupPlatform(id string) bool { return id == PlatformComposite || IsConcretePlatform(id) }
