package catalog

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	modelcapabilities "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-capabilities"
	"github.com/stretchr/testify/require"
)

func TestPiCapabilitySupplementFormat(t *testing.T) {
	var document struct {
		SchemaVersion int    `json:"schema_version"`
		SourcePackage string `json:"source_package"`
		SourceVersion string `json:"source_version"`
		Records       []struct {
			builtinModel
			Profile string `json:"profile"`
		} `json:"records"`
	}
	decoder := json.NewDecoder(bytes.NewReader(modelcapabilities.PiSupplementJSON()))
	// The capability schema accepts only specification fields.
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&document))
	require.Equal(t, 1, document.SchemaVersion)
	require.Equal(t, "@mariozechner/pi-ai", document.SourcePackage)
	require.NotEmpty(t, document.SourceVersion)
	require.Greater(t, len(document.Records), 500)
	previous := ""
	for _, model := range document.Records {
		require.Equal(t, model.Provider, model.Profile)
		require.NotEmpty(t, model.Model)
		require.NotEmpty(t, model.DisplayName)
		require.NotEmpty(t, model.Methods)
		require.Positive(t, model.Context)
		require.Equal(t, "total", model.ContextKind)
		require.Positive(t, model.Output)
		require.NotNil(t, model.Reasoning)
		require.NotNil(t, model.Vision)
		require.NotEmpty(t, model.Inputs)
		require.Equal(t, []string{"text"}, model.Outputs)
		key := model.Provider + "/" + model.Model
		require.Less(t, previous, key)
		previous = key
	}
}

func TestPiCapabilitySupplementPreservesOmniHiveFacts(t *testing.T) {
	previous := approvedModels.Load()
	t.Cleanup(func() { approvedModels.Store(previous) })
	// Reviewed facts enter through the registry projection, never by decoding
	// catalog.json in this module. Include an explicit suppressed specification.
	context := int64(1048576)
	PublishApprovedInventory("reviewed-fixture", []RegistryModel{
		{Provider: "gemini", Model: "gemini-3-pro-preview", Facts: ModelFacts{Facts: Facts{Context: &context, ContextKind: "total", Modalities: []string{"text", "image", "audio", "video", "pdf"}}, Evidence: Evidence{Source: SourceBuiltin}}, Capabilities: []Capability{{Protocol: "gemini_generate"}}},
		{Provider: "moonshotai-cn", Model: "kimi-k2.6", Facts: ModelFacts{Evidence: Evidence{Source: SourceBuiltin}}},
	})
	var supplement struct {
		Records []builtinModel `json:"records"`
	}
	require.NoError(t, json.Unmarshal(modelcapabilities.PiSupplementJSON(), &supplement))
	require.Contains(t, supplement.Records, builtinModel{
		Provider: "gemini", Model: "gemini-3-pro-preview", DisplayName: "Gemini 3 Pro Preview",
		Mode: "chat", Context: 1000000, ContextKind: "total", Output: 64000,
		Methods: []string{"generateContent"}, Inputs: []string{"text", "image"}, Outputs: []string{"text"},
		Reasoning: new(true), Vision: new(true),
	})

	models := builtinModels()
	reviewed := models[[2]string{"gemini", "gemini-3-pro-preview"}]
	require.False(t, reviewed.fromPi)
	require.Equal(t, int64(1048576), reviewed.Context)
	require.Equal(t, []string{"text", "image", "audio", "video", "pdf"}, reviewed.Inputs)
	require.Empty(t, reviewed.Outputs, "the projection does not invent a separate output-modality fact")
	require.Equal(t, []string{"generateContent"}, BuiltinMethods(reviewed.Provider, reviewed.Model))

	custom := models[[2]string{"openai", "agy-cli/gemini-4-carbon"}]
	require.False(t, custom.fromPi)
	require.Equal(t, int64(1000000), custom.Context)
	require.Equal(t, []string{"text", "image", "audio", "video", "pdf"}, custom.Inputs)
	require.Equal(t, []string{"text"}, custom.Outputs)
	require.Equal(t, []string{"chat_completions"}, BuiltinMethods(custom.Provider, custom.Model))

	suppressed := models[[2]string{"moonshotai-cn", "kimi-k2.6"}]
	require.Equal(t, "kimi-k2.6", suppressed.Model)
	require.False(t, suppressed.fromPi)
	require.Zero(t, suppressed.Context)
	require.Zero(t, suppressed.Input)
	require.Zero(t, suppressed.Output)
}

func TestPiCapabilitySupplementProjectsNewModelWithExactProvider(t *testing.T) {
	const id = "gemini-1.5-flash"
	const provider = "gemini"
	require.Equal(t, []string{"generateContent"}, BuiltinMethods(provider, id))
	require.Empty(t, BuiltinMethods("openai", id))
	require.Equal(t, []string{"chat_completions"}, BuiltinMethods("openrouter", "openai/gpt-5"))
	require.Empty(t, BuiltinMethods("openai", "openai/gpt-5"))

	input := Input{Models: []Candidate{{Provider: provider, Model: id}}}
	detail := Project(Perspective{}, input, Window{}, time.Now()).Details[0]
	require.Equal(t, "unpriced", detail.Model.PriceState)
	require.Nil(t, detail.Model.Input)
	require.NotNil(t, detail.ReferencePrice)
	require.Equal(t, "pi-ai", detail.ReferencePrice.Source)
	require.Equal(t, "0.000000075", detail.ReferencePrice.Input.Amount().String())
	require.Equal(t, "Gemini 1.5 Flash", detail.Model.DisplayName)
	require.ElementsMatch(t, []string{"text", "image"}, detail.Model.Modalities)
	require.Equal(t, []string{"gemini_generate"}, detail.Model.Protocols)
	require.Equal(t, "requires_scope", detail.Model.RouteState)
	require.NotNil(t, detail.Model.Context)
	require.EqualValues(t, 1000000, *detail.Model.Context)
	require.Len(t, detail.Facts, 1)
	facts := detail.Facts[0]
	require.Equal(t, SourcePI, facts.Source)
	require.Equal(t, "reference", facts.Provenance["context_tokens"].Approval)
	require.Equal(t, "total", facts.ContextKind)
	require.Equal(t, new(int64(8192)), facts.Output)
	require.Equal(t, new(false), facts.Reasoning)
	require.Equal(t, new(true), facts.Vision)
}
