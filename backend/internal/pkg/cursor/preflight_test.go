package cursor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunPreflightFailover(t *testing.T) {
	for _, scenario := range []string{"gating", "gating_after_heartbeat", "connect", "already_text", "already_thinking"} {
		t.Run(scenario, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, err := readFrame(request.Body)
				if err != nil {
					return
				}
				attempt := attempts.Add(1)
				writer.Header().Set("Content-Type", "application/connect+proto")
				write := func(payload message) { _, _ = writer.Write(encodeFrame(payload)); writer.(http.Flusher).Flush() }
				textFrame := func(kind uint64, text string) message {
					var delta, interaction, envelope message
					delta.text(1, text)
					interaction.bytes(kind, delta)
					envelope.bytes(serverInteraction, interaction)
					return envelope
				}
				if attempt == 1 {
					switch scenario {
					case "gating_after_heartbeat":
						var interaction, envelope message
						interaction.bytes(13, nil)
						envelope.bytes(serverInteraction, interaction)
						write(envelope)
					case "already_text":
						write(textFrame(interactionText, "partial"))
					case "already_thinking":
						write(textFrame(interactionThinking, "partial"))
					}
					if scenario == "connect" || strings.HasPrefix(scenario, "already_") {
						payload := encodeFrame([]byte(`{"error":{"code":"internal","message":"synthetic"}}`))
						payload[0] = 2
						_, _ = writer.Write(payload)
						writer.(http.Flusher).Flush()
					} else {
						write(textFrame(interactionText, "ERROR_DEPRECATED Update Required"))
					}
					return
				}
				write(textFrame(interactionText, "hello"))
				var usage, interaction, envelope message
				usage.number(usageInput, 12)
				usage.number(usageOutput, 5)
				interaction.bytes(interactionTurnEnded, usage)
				envelope.bytes(serverInteraction, interaction)
				write(envelope)
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			client := &Client{HTTPClient: server.Client(), RunHosts: []string{server.URL, server.URL}}
			var output strings.Builder
			usage, err := client.Run(context.Background(), Credentials{}, []ChatMessage{{Role: "user", Content: "hi"}}, "default", func(event StreamEvent) error { output.WriteString(event.Text); return nil })
			if strings.HasPrefix(scenario, "already_") {
				require.Error(t, err)
				require.Equal(t, int32(1), attempts.Load())
				require.Equal(t, "partial", output.String())
			} else {
				require.NoError(t, err)
				require.Equal(t, int32(2), attempts.Load())
				require.Equal(t, "hello", output.String())
				require.Equal(t, 5, usage.OutputTokens)
				require.False(t, usage.Estimated)
			}
		})
	}
}

func TestRunUsageAuthorityAndFallback(t *testing.T) {
	for _, ended := range []bool{false, true} {
		t.Run(fmt.Sprint(ended), func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, err := readFrame(request.Body)
				if err != nil {
					return
				}
				for _, tokens := range []int{2, 7} {
					var delta, interaction, envelope message
					delta.number(1, uint64(tokens))
					interaction.bytes(interactionTokens, delta)
					envelope.bytes(serverInteraction, interaction)
					_, _ = writer.Write(encodeFrame(envelope))
				}
				if ended {
					var usage, interaction, envelope message
					usage.number(usageOutput, 5)
					usage.number(usageReasoning, 3)
					usage.number(usageCacheWrite, 2)
					interaction.bytes(interactionTurnEnded, usage)
					envelope.bytes(serverInteraction, interaction)
					_, _ = writer.Write(encodeFrame(envelope))
				}
				writer.(http.Flusher).Flush()
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			usage, err := (&Client{HTTPClient: server.Client(), BaseURL: server.URL}).Run(context.Background(), Credentials{}, nil, "default", func(StreamEvent) error { return io.ErrClosedPipe })
			require.NoError(t, err)
			require.Equal(t, !ended, usage.Estimated)
			if ended {
				require.Equal(t, 5, usage.OutputTokens)
				require.Equal(t, 3, usage.ReasoningTokens)
				require.Equal(t, 2, usage.CacheWriteTokens)
			} else {
				require.Equal(t, 9, usage.OutputTokens)
			}
		})
	}
}
