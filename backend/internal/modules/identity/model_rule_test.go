package identity

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestModelRuleNilEmptyNamedAndExclude(t *testing.T) {
	group := int64(1)
	require.Nil(t, ResolveModelRule(ModelRule{}, nil))
	require.Equal(t, []string{}, ResolveModelRule(ModelRule{Models: []string{}}, nil))
	require.Equal(t, []string{}, ResolveModelRule(ModelRule{AccessGroupID: &group}, nil))
	require.Equal(t, []string{"a"}, ResolveModelRule(ModelRule{AccessGroupID: &group, Exclude: []string{"b"}}, []string{"a", "b"}))
	require.Equal(t, []string{}, ResolveModelRule(ModelRule{AccessGroupID: &group, Models: []string{"c"}}, []string{"a"}))
	p := KeyPolicy{Actions: []string{"text.chat", "models.read"}, Mode: "inherit", Platform: "openai", ExcludedModels: []string{"private"}}
	require.False(t, p.Allows("text.chat", []string{"private"}))
	require.True(t, p.Allows("text.chat", []string{"public"}))
	require.True(t, p.Allows("models.read", nil))
	require.False(t, p.Allows("image.generate", []string{"public"}))
}
