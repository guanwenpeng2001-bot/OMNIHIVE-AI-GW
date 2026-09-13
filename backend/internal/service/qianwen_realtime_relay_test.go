//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

const realtimeConfig = `{"type":"session.update","session":{"mode":"commit","response_format":"pcm","sample_rate":24000,"voice":"Cherry"}}`
const realtimeCreated = `{"type":"session.created","session":{"id":"provider-session","model":"qwen3-tts-flash-realtime"}}`

func TestQianwenRealtimeStateRejectsUnsupportedModesAndBounds(t *testing.T) {
	for _, raw := range []string{`{"type":"response.create"}`, `{"type":"input_audio_buffer.append"}`, `{"type":"session.update","session":{"mode":"server_commit"}}`, strings.Replace(realtimeConfig, `"voice":"Cherry"`, `"model":"qwen3-omni-flash-realtime"`, 1), `{"type":"input_text_buffer.commit"}`} {
		state := qianwenRealtimeState{}
		require.Error(t, state.client([]byte(raw), time.Now()), raw)
	}
	state := qianwenRealtimeState{}
	require.NoError(t, state.client([]byte(realtimeConfig), time.Now()))
	require.Error(t, state.client([]byte(`{"type":"input_text_buffer.append","text":"Hello"}`), time.Now()), "wait for session.updated")
	state.configured = true
	state.configuring = false
	require.NoError(t, state.client([]byte(`{"type":"input_text_buffer.append","text":"Hello"}`), time.Now()))
	require.NoError(t, state.client([]byte(`{"type":"input_text_buffer.commit"}`), time.Now()))
	require.Error(t, state.client([]byte(`{"type":"input_text_buffer.commit"}`), time.Now()))
	require.Error(t, state.client([]byte(`{"type":"input_text_buffer.clear"}`), time.Now()))
	state.generating = false
	state.characters = 2000
	require.Error(t, state.client([]byte(`{"type":"input_text_buffer.append","text":"a"}`), time.Now()))
	state.windowEvents = 50
	require.Error(t, state.client([]byte(`{"type":"session.finish"}`), time.Now()))
	state.window = time.Time{}
	state.events = 2000
	require.Error(t, state.client([]byte(`{"type":"session.finish"}`), time.Now()))
}

func TestQianwenRealtimeHandshakeFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		event  string
	}{{"auth", 401, ""}, {"forbidden", 403, ""}, {"wrong_event", 101, `{"type":"error"}`}, {"wrong_model", 101, strings.Replace(realtimeCreated, "qwen3-tts-flash-realtime", "other", 1)}, {"success", 101, realtimeCreated}} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer provider-key", r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("Sec-WebSocket-Extensions"))
				if test.status != 101 {
					w.WriteHeader(test.status)
					return
				}
				conn, err := ws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				_ = conn.Write(r.Context(), ws.MessageText, []byte(test.event))
				_, _, _ = conn.Read(r.Context())
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			upstream, err := dialQianwenRealtime(ctx, strings.Replace(server.URL, "http", "ws", 1), "qwen3-tts-flash-realtime", http.Header{"Authorization": {"Bearer provider-key"}}, server.Client())
			if test.name == "success" {
				require.NoError(t, err)
				upstream.Close()
			} else {
				require.Error(t, err)
				require.Nil(t, upstream)
				if test.status != 101 {
					var dialErr *QianwenRealtimeDialError
					require.ErrorAs(t, err, &dialErr)
					require.Equal(t, test.status, dialErr.StatusCode)
				}
			}
		})
	}
	t.Run("first_event_timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := ws.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			_, _, _ = conn.Read(r.Context())
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err := dialQianwenRealtime(ctx, strings.Replace(server.URL, "http", "ws", 1), "qwen3-tts-flash-realtime", nil, server.Client())
		require.Error(t, err)
	})
}

// These are isolated mock socket peers, never another gateway or a shared DB.
func realtimeSocketPair(t *testing.T) (*ws.Conn, *ws.Conn) {
	t.Helper()
	accepted := make(chan *ws.Conn, 1)
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
		<-done
		_ = conn.CloseNow()
	}))
	conn, _, err := ws.Dial(context.Background(), strings.Replace(server.URL, "http", "ws", 1), nil)
	require.NoError(t, err)
	peer := <-accepted
	t.Cleanup(func() { _ = conn.CloseNow(); _ = peer.CloseNow(); close(done); server.Close() })
	return conn, peer
}

func TestQianwenRealtimeTwoTurnsReplayAndDisconnectDrain(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(map[bool]string{true: "disconnect_after_commit", false: "two_turns_and_replay"}[disconnect], func(t *testing.T) {
			client, gatewayClient := realtimeSocketPair(t)
			gatewayUpstream, provider := realtimeSocketPair(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
			svc := NewUpstreamJobService(repo, nil, QianwenAsyncJobCharge)
			var commits atomic.Int32
			finished := make(chan error, 1)
			go func() {
				finished <- relayQianwenRealtime(ctx, gatewayClient, &QianwenRealtimeUpstream{Conn: gatewayUpstream, Created: []byte(realtimeCreated)}, QianwenRealtimeCallbacks{
					BeforeCommit: func(context.Context, string) error { commits.Add(1); return nil },
					Receipt: func(ctx context.Context, id string, usage []byte, status string) error {
						_, err := svc.RecordQianwenRealtimeTurn(ctx, realtimeSessionFixture(), id, usage, status)
						return err
					},
				}, time.Second, time.Second, 300*time.Millisecond)
			}()
			read := func(conn *ws.Conn) []byte { _, b, err := conn.Read(ctx); require.NoError(t, err); return b }
			write := func(conn *ws.Conn, s string) { require.NoError(t, conn.Write(ctx, ws.MessageText, []byte(s))) }
			require.JSONEq(t, realtimeCreated, string(read(client)))
			write(client, realtimeConfig)
			require.JSONEq(t, realtimeConfig, string(read(provider)))
			updated := `{"type":"session.updated","session":{"mode":"commit"}}`
			write(provider, updated)
			read(client)
			turns := 2
			if disconnect {
				turns = 1
			}
			for turn := 1; turn <= turns; turn++ {
				write(client, `{"type":"input_text_buffer.append","text":"Hello"}`)
				read(provider)
				write(client, `{"type":"input_text_buffer.commit"}`)
				read(provider)
				id := []string{"response-one", "response-two"}[turn-1]
				created, _ := json.Marshal(map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
				write(provider, string(created))
				read(client)
				if disconnect {
					_ = client.CloseNow()
					read(provider)
				}
				done, _ := json.Marshal(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed", "usage": map[string]int{"characters": 5}}})
				write(provider, string(done))
				if !disconnect {
					read(client)
					if turn == 1 {
						write(provider, string(done))
						read(client)
					}
				}
			}
			if !disconnect {
				write(client, `{"type":"session.finish"}`)
				read(provider)
				write(provider, `{"type":"session.finished"}`)
				read(client)
			}
			select {
			case err := <-finished:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("relay readers did not stop")
			}
			require.Equal(t, turns, repo.charges)
			require.Equal(t, int32(turns), commits.Load())
		})
	}
}

func TestQianwenRealtimeTimeoutAndDisconnectCleanup(t *testing.T) {
	for _, test := range []struct {
		name              string
		life, idle, drain time.Duration
		disconnect        bool
	}{{"absolute", 30 * time.Millisecond, time.Second, 20 * time.Millisecond, false}, {"idle", time.Second, 30 * time.Millisecond, 20 * time.Millisecond, false}, {"disconnect", time.Second, time.Second, 20 * time.Millisecond, true}} {
		t.Run(test.name, func(t *testing.T) {
			client, gatewayClient := realtimeSocketPair(t)
			gatewayUpstream, _ := realtimeSocketPair(t)
			done := make(chan error, 1)
			go func() {
				done <- relayQianwenRealtime(context.Background(), gatewayClient, &QianwenRealtimeUpstream{Conn: gatewayUpstream, Created: []byte(realtimeCreated)}, QianwenRealtimeCallbacks{BeforeCommit: func(context.Context, string) error { return nil }, Receipt: func(context.Context, string, []byte, string) error { return errors.New("unexpected receipt") }}, test.life, test.idle, test.drain)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _, err := client.Read(ctx)
			require.NoError(t, err)
			if test.disconnect {
				_ = client.CloseNow()
			}
			select {
			case err := <-done:
				require.Error(t, err)
			case <-ctx.Done():
				t.Fatal("timeout did not join pumps")
			}
		})
	}
}
