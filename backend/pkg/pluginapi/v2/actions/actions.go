// Package actions defines explicit v2 grants for host-managed adapters.
package actions

const (
	ProviderResponses = "provider.responses.v1"
	TaskImageGenerate = "task.image.generate.v1"
)

func (k Kind) Capability() string {
	switch k {
	case Kind_PROVIDER_RESPONSES:
		return ProviderResponses
	case Kind_TASK_IMAGE_GENERATE:
		return TaskImageGenerate
	default:
		return ""
	}
}

// ValidFor rejects a different adapter, an unknown enum or an unrecognized
// protobuf field before applying a guest's transfer preference.
func (p *Plan) ValidFor(kind Kind) bool {
	return p != nil && kind.Capability() != "" && p.Kind == kind && len(p.ProtoReflect().GetUnknown()) == 0 &&
		(p.Transfer == Transfer_HOST_DEFAULT || p.Transfer == Transfer_IDENTITY)
}
