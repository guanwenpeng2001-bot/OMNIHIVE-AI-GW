package cursor

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

var errToolReply = errors.New("cursor: MCP reply failed")

const RunEndpoint = "/agent.v1.AgentService/Run"

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	// RunHosts optionally overrides native failover hosts; BaseURL remains the single-host override.
	RunHosts []string
}

func NewClient() *Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	return newClientWithDial(dialer.DialContext)
}

func newClientWithDial(dial func(context.Context, string, string) (net.Conn, error)) *Client {
	transport := &http2.Transport{DialTLSContext: func(ctx context.Context, network, address string, _ *tls.Config) (net.Conn, error) {
		connection, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.Join(err, connection.Close())
		}
		secured := utls.UClient(connection, &utls.Config{ServerName: host}, utls.HelloChrome_Auto)
		if err := secured.HandshakeContext(ctx); err != nil {
			return nil, errors.Join(err, connection.Close())
		}
		if secured.ConnectionState().NegotiatedProtocol != "h2" {
			return nil, errors.Join(fmt.Errorf("cursor: HTTP/2 negotiation failed"), connection.Close())
		}
		return secured, nil
	}}
	return &Client{HTTPClient: &http.Client{Transport: transport, Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, BaseURL: ""}
}

// Run owns the duplex request body and heartbeat lifetime. A single writer
// serializes frames; pipe closure never waits on its mutex, so cancellation can
// interrupt blocked writes. The callback only receives typed assistant events.
func (client *Client) Run(ctx context.Context, credentials Credentials, messages []ChatMessage, model string, emit func(StreamEvent) error) (TokenUsage, error) {
	return client.RunWithTools(ctx, credentials, messages, model, ToolOptions{}, emit)
}

func (client *Client) RunWithTools(ctx context.Context, credentials Credentials, messages []ChatMessage, model string, options ToolOptions, emit func(StreamEvent) error) (TokenUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	hosts := []string{client.BaseURL}
	if len(client.RunHosts) > 0 {
		hosts = client.RunHosts
	} else if client.BaseURL == "" {
		hosts = []string{"https://agentn.global.api5.cursor.sh", "https://agentn.api5.cursor.sh", "https://agentn-gcpp-eucentral.api5.cursor.sh"}
	}
	// Run is a non-idempotent POST. A transport failure or stream rejection
	// does not prove the request was unaccepted; never regenerate on another host.
	attempt := *client
	attempt.BaseURL = hosts[0]
	return attempt.run(ctx, credentials, messages, model, options, emit)
}

func (client *Client) run(ctx context.Context, credentials Credentials, messages []ChatMessage, model string, options ToolOptions, emit func(StreamEvent) error) (TokenUsage, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	// io.Pipe Close and CloseWithError always return nil; the supplied error
	// is delivered to the peer, including when cancellation interrupts a write.
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	stop := context.AfterFunc(ctx, func() {
		_ = writer.CloseWithError(ctx.Err())
		_ = reader.CloseWithError(ctx.Err())
	})
	defer stop()
	payload, runID := encodeRun(messages, model, options.Definitions...)
	recordRunDiagnostics(ctx, messages, model, len(options.Definitions), len(payload))
	var writeMu sync.Mutex
	write := func(payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err := writer.Write(encodeFrame(payload))
		return err
	}
	go func() {
		if err := write(payload); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var heartbeat message
				heartbeat.bytes(clientHeartbeat, nil)
				if err := write(heartbeat); err != nil {
					return
				}
			}
		}
	}()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(client.BaseURL, "/")+RunEndpoint, reader)
	if err != nil {
		return TokenUsage{}, fmt.Errorf("cursor: invalid Run endpoint")
	}
	request.Header = headers(credentials, runID)
	if len(options.Definitions) > 0 {
		request.Header.Set("X-Cursor-Agent-Allowed-Tools", "mcp_tool_call,get_mcp_tools_tool_call")
	}
	response, err := sendGeneration(client.HTTPClient, request)
	if err != nil {
		return TokenUsage{}, &TransportError{cause: err}
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			slog.Warn("Cursor Run response cleanup failed")
		}
	}()
	if response.StatusCode != http.StatusOK {
		return TokenUsage{}, &HTTPError{Status: response.StatusCode}
	}
	blobs := make(map[string][]byte)
	storedBytes := 0
	usage := TokenUsage{Estimated: true}
	outputStarted := false
	for {
		frame, err := readFrame(response.Body)
		if err != nil {
			if err == io.EOF {
				return usage, fmt.Errorf("cursor: stream ended before turn_ended: %w", io.ErrUnexpectedEOF)
			}
			return usage, err
		}
		// Every frame before the first assistant token is a preflight frame:
		// heartbeats and KV exchanges may precede a rejection. Inspect the entire
		// frame before emitting any of its events so a later gating field in the
		// same frame cannot leak earlier text to the downstream client.
		if !outputStarted && isRejectedCursorStream(frame.payload) {
			return usage, fmt.Errorf("cursor: upstream rejected stream; update client version or retry another host")
		}
		if frame.flags&2 != 0 {
			err := trailerError(frame.payload)
			if err == nil {
				err = fmt.Errorf("cursor: stream ended before turn_ended")
			}
			return usage, err
		}
		reply, handled, err := replyKV(frame.payload, blobs, &storedBytes)
		if err != nil {
			return usage, err
		}
		if handled {
			if err := write(reply); err != nil {
				return usage, err
			}
			continue
		}
		metadataReply, metadataHandled, err := replyMCPMetadata(frame.payload, options.Definitions)
		if err != nil {
			return usage, err
		}
		if metadataHandled {
			if err := write(metadataReply); err != nil {
				return usage, fmt.Errorf("cursor: MCP metadata reply failed: %w", err)
			}
			continue
		}
		call, err := parseToolCall(frame.payload)
		if err != nil {
			return usage, err
		}
		if call != nil {
			outputStarted = true
			result, failure := ToolResult{}, "deferred_to_client"
			if options.Execute != nil {
				result, err = options.Execute(ctx, *call)
				failure = ""
				if err != nil {
					failure = "tool_execution_failed"
				}
			}
			if err := write(encodeToolReply(*call, result, failure)); err != nil {
				return usage, fmt.Errorf("%w: %w", errToolReply, err)
			}
			if err := emit(StreamEvent{Type: "tool_call", ToolCall: call}); err != nil {
				return usage, err
			}
			continue
		}
		events, err := parseEvents(frame.payload)
		if err != nil {
			return usage, err
		}
		for _, event := range events {
			switch event.Type {
			case "token_delta":
				usage.OutputTokens += event.Usage.OutputTokens
			case "turn_ended":
				if !event.Usage.Estimated {
					usage = event.Usage
				}
				return usage, nil
			case "text", "thinking":
				if event.Text != "" {
					outputStarted = true
				}
				if err := emit(event); err != nil {
					return usage, err
				}
			}
		}
	}
}

// Recognize only platform gating diagnostics, before any assistant output.
// Never return the payload: upstream diagnostics can include private context.
func isRejectedCursorStream(payload []byte) bool {
	text := strings.ToLower(string(payload))
	for _, marker := range []string{"error_deprecated", "request type deprecated", "outdated version of cursor", "update required", "error_gpt_4_vision_preview_rate_limit"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
