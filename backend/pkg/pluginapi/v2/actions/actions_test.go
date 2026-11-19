package actions

import "testing"

func TestTypedPlan(t *testing.T) {
	extra := &Plan{Kind: Kind_PROVIDER_RESPONSES, Transfer: Transfer_IDENTITY}
	extra.ProtoReflect().SetUnknown([]byte{0x18, 0x01})
	for _, tc := range []struct {
		name string
		plan *Plan
		good bool
	}{
		{"missing", nil, false}, {"unselected", &Plan{}, false},
		{"other capability", &Plan{Kind: Kind_TASK_IMAGE_GENERATE, Transfer: Transfer_IDENTITY}, false},
		{"unknown transfer", &Plan{Kind: Kind_PROVIDER_RESPONSES, Transfer: Transfer(99)}, false},
		{"unknown field", extra, false},
		{"native default", &Plan{Kind: Kind_PROVIDER_RESPONSES, Transfer: Transfer_HOST_DEFAULT}, true},
		{"identity", &Plan{Kind: Kind_PROVIDER_RESPONSES, Transfer: Transfer_IDENTITY}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.plan.ValidFor(Kind_PROVIDER_RESPONSES); got != tc.good {
				t.Errorf("valid = %v, want %v", got, tc.good)
			}
		})
	}
}
