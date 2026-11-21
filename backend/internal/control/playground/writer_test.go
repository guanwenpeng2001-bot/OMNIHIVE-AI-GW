package playground

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	pg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/playground"
	"github.com/stretchr/testify/require"
)

func TestNativeProtocols(t *testing.T) {
	cases, err := os.ReadFile("../../../../contracts/protocol/playground/native.json")
	require.NoError(t, err)
	var rows []struct {
		Protocol      string
		Input, Output json.RawMessage
		Frames        []json.RawMessage
	}
	require.NoError(t, json.Unmarshal(cases, &rows))
	for _, row := range rows {
		t.Run(row.Protocol, func(t *testing.T) {
			in, _, err := pg.Parse([]byte(`{"model_id":"m","api_key_id":"1","funding_acknowledged":true,"action":"text.chat","protocol":"`+row.Protocol+`","input":`+string(row.Input)+`}`), "key", "")
			require.NoError(t, err)
			native, err := in.Native("model")
			require.NoError(t, err)
			require.Equal(t, 128, native.OutputBound)
			require.Positive(t, native.InputBound)
			var events []pg.Event
			w := &nativeResponse{header: http.Header{}, decoder: pg.Decoder{Protocol: row.Protocol}, stream: true, raw: true, cancel: func() {}, emit: func(e pg.Event) error { events = append(events, e); return nil }}
			for _, frame := range row.Frames {
				var obj map[string]any
				raw := "[DONE]"
				if json.Unmarshal(frame, &obj) == nil {
					obj["private_debug"] = "sensitive upstream detail"
					b, _ := json.Marshal(obj)
					raw = string(b)
				}
				for _, b := range []byte(": ping\r\ndata: " + raw + "\r\n\r\n") {
					_, err = w.Write([]byte{b})
					require.NoError(t, err)
				}
			}
			require.True(t, w.decoder.Completed)
			body, _ := json.Marshal(events)
			for _, fragment := range []string{"fn1", "lookup", "reasoning", "provider_reported", "input_token", "raw_event"} {
				require.Contains(t, string(body), fragment)
			}
			require.NotContains(t, string(body), "sensitive upstream detail")
			out, ok := pg.SyncOutput(row.Protocol, row.Output)
			require.True(t, ok)
			require.JSONEq(t, string(row.Output), string(out))
		})
	}
}
func TestNativeMalformedBounds(t *testing.T) {
	_, cancel := context.WithCancel(t.Context())
	defer cancel()
	w := &nativeResponse{header: http.Header{}, decoder: pg.Decoder{Protocol: "openai_chat"}, stream: true, cancel: cancel}
	_, err := w.Write(bytes.Repeat([]byte{'x'}, maxFrame+1))
	require.Error(t, err)
	require.False(t, w.decoder.Completed)
	for _, bad := range []string{`"max_tokens":32769`, `"n":5`, `"stream":false`, `"model":"other"`} {
		body := `{"api_key_id":"1","funding_acknowledged":true,"action":"text.chat","model_id":"m","protocol":"openai_chat","input":{"messages":[{"role":"user","content":"ok"}],` + bad + `}}`
		in, _, err := pg.Parse([]byte(body), "key", "")
		require.NoError(t, err)
		_, err = in.Native("m")
		require.Error(t, err)
	}
	_, _, err = pg.Parse([]byte(`{"input":{"a":1,"a":2}}`), "key", "")
	require.Error(t, err)
	d := pg.Decoder{Protocol: "openai_responses"}
	e, err := d.Frame("", `{"type":"response.failed","error":{"message":"private data"}}`)
	require.NoError(t, err)
	require.Empty(t, e)
	require.True(t, d.Failed)
	h := New(nil, false)
	h.PreviewTTL = 10 * time.Millisecond
	h.retain("run", "local")
	_, found := h.previews.Load("run")
	require.True(t, found)
	require.Eventually(t, func() bool { _, ok := h.previews.Load("run"); return !ok }, time.Second, time.Millisecond)
}

func TestSelectModelMatchesCanonicalIDOrUnambiguousSlug(t *testing.T) {
	snap := catalog.Snapshot{
		Details: []catalog.Detail{
			{
				Model:   catalog.Summary{ID: "m_11111111111111111111111111111111", Model: "auto/gemini-3.8-flash", Provider: "openai", AccessState: "allowed", RouteState: "eligible"},
				Aliases: []string{"gemini-3.8-flash"},
			},
			{
				Model: catalog.Summary{ID: "m_22222222222222222222222222222222", Model: "shared-slug", Provider: "openai", AccessState: "allowed", RouteState: "eligible"},
			},
			{
				Model: catalog.Summary{ID: "m_33333333333333333333333333333333", Model: "shared-slug", Provider: "anthropic", AccessState: "allowed", RouteState: "eligible"},
			},
		},
	}

	byID, err := selectModel(snap, "m_22222222222222222222222222222222", false)
	require.NoError(t, err)
	require.Equal(t, "openai", byID.Model.Provider)

	bySlug, err := selectModel(snap, "auto/gemini-3.8-flash", false)
	require.NoError(t, err)
	require.Equal(t, "m_11111111111111111111111111111111", bySlug.Model.ID)

	byAlias, err := selectModel(snap, "gemini-3.8-flash", false)
	require.NoError(t, err)
	require.Equal(t, "m_11111111111111111111111111111111", byAlias.Model.ID)

	_, err = selectModel(snap, "shared-slug", false)
	require.ErrorIs(t, err, unavailableModel)
}
