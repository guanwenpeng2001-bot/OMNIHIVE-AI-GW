package cursor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// ToolIdentityTTL allows a client 45 minutes to finish a deferred tool turn,
// while bounding retention of opaque upstream execution identifiers.
const ToolIdentityTTL = 45 * time.Minute

const toolIdentityKeyPrefix = "cursor:tool-identity:v1:"

// ToolIdentity contains only the metadata needed to restore a rewritten ID.
type ToolIdentity struct {
	CallID    string    `json:"call_id"`
	ExecID    string    `json:"exec_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type ToolIdentityStore interface {
	Put(context.Context, string, ToolIdentity) error
	Get(context.Context, string) (ToolIdentity, bool, error)
}

type redisToolIdentityStore struct{ client *redis.Client }

// NewRedisToolIdentityStore reuses the application's Redis client. The global
// namespace matches IDs hashed from both CallID and ExecID, including failover.
func NewRedisToolIdentityStore(client *redis.Client) ToolIdentityStore {
	return &redisToolIdentityStore{client: client}
}

func (store *redisToolIdentityStore) Put(ctx context.Context, id string, identity ToolIdentity) error {
	data, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	ttl := time.Until(identity.ExpiresAt)
	if ttl <= 0 {
		return nil
	}
	return store.client.Set(ctx, toolIdentityKeyPrefix+id, data, ttl).Err()
}

func (store *redisToolIdentityStore) Get(ctx context.Context, id string) (ToolIdentity, bool, error) {
	var identity ToolIdentity
	data, err := store.client.Get(ctx, toolIdentityKeyPrefix+id).Bytes()
	if errors.Is(err, redis.Nil) {
		return identity, false, nil
	}
	if err != nil {
		return identity, false, err
	}
	if err := json.Unmarshal(data, &identity); err != nil {
		return identity, false, err
	}
	if identity.CallID == "" || identity.ExecID == "" || NormalizeToolCallID(identity.CallID+"\x00"+identity.ExecID) != id {
		return ToolIdentity{}, false, fmt.Errorf("cursor: invalid stored tool identity")
	}
	return identity, time.Now().Before(identity.ExpiresAt), nil
}

// NormalizeToolCallID preserves ordinary API IDs and hashes opaque Cursor IDs
// into a stable, bounded ASCII identifier. The gateway retains the original
// ToolCall separately; wire replies must always use its unmodified ExecID.
func NormalizeToolCallID(id string) string {
	valid := len(id) > 0 && len(id) <= 64 && !strings.HasPrefix(id, "cursor_call_")
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			valid = false
		}
	}
	if valid {
		return id
	}
	digest := sha256.Sum256([]byte(id))
	return "cursor_call_" + base64.RawURLEncoding.EncodeToString(digest[:])
}

// ToolDefinition is a custom MCP tool. Name is the downstream API name;
// Server identifies the MCP provider (the gateway uses "api").
type ToolDefinition struct {
	Name, Description, Server string
	InputSchema               json.RawMessage
}

type ToolCall struct {
	ID                                                                   uint32
	ExecID, CallID, Name, ProviderIdentifier, ToolName, ServerIdentifier string
	Arguments                                                            json.RawMessage
}

type ToolResult struct {
	Text    []string
	IsError bool
}

type ToolOptions struct {
	Definitions []ToolDefinition
	// A nil Execute defers execution to the downstream API client. Errors are
	// acknowledged with a fixed message so executor errors cannot leak secrets.
	Execute func(context.Context, ToolCall) (ToolResult, error)
}

func encodeTools(tools []ToolDefinition) message {
	var result message
	for _, tool := range tools {
		var definition message
		definition.text(1, tool.Server+"-"+tool.Name)
		definition.text(2, tool.Description)
		definition.text(4, tool.Server)
		definition.text(5, tool.Name)
		// Field 3 differs between published schemas; field 6 is verified JSON.
		definition.text(6, string(tool.InputSchema))
		result.bytes(1, definition)
	}
	return result
}

func parseToolCall(payload []byte) (*ToolCall, error) {
	decoded, err := fields(payload)
	if err != nil {
		return nil, err
	}
	exec := findField(decoded, 2)
	if exec.number == 0 {
		return nil, nil
	}
	if exec.wire != wireBytes {
		return nil, fmt.Errorf("cursor: malformed exec")
	}
	details, err := fields(exec.data)
	if err != nil {
		return nil, err
	}
	args := findField(details, 11)
	if args.number == 0 {
		return nil, fmt.Errorf("cursor: unsupported exec operation")
	}
	id, execID := findField(details, 1), findField(details, 15)
	if (id.number != 0 && (id.wire != wireVarint || id.integer > 0xffffffff)) || execID.wire != wireBytes || args.wire != wireBytes {
		return nil, fmt.Errorf("cursor: malformed MCP execution identity")
	}
	values, err := fields(args.data)
	if err != nil {
		return nil, err
	}
	call := &ToolCall{ID: uint32(id.integer), ExecID: string(execID.data), Name: string(findField(values, 1).data), CallID: string(findField(values, 3).data), ProviderIdentifier: string(findField(values, 4).data), ToolName: string(findField(values, 5).data), ServerIdentifier: string(findField(values, 9).data)}
	arguments := map[string]any{}
	for _, value := range values {
		if value.number != 2 {
			continue
		}
		if value.wire != wireBytes {
			return nil, fmt.Errorf("cursor: malformed MCP argument")
		}
		entry, err := fields(value.data)
		if err != nil {
			return nil, err
		}
		key, data := findField(entry, 1), findField(entry, 2)
		if key.wire != wireBytes || data.wire != wireBytes {
			return nil, fmt.Errorf("cursor: malformed MCP argument entry")
		}
		var decoded structpb.Value
		if err := proto.Unmarshal(data.data, &decoded); err != nil || decoded.Kind == nil {
			return nil, fmt.Errorf("cursor: invalid MCP argument value")
		}
		arguments[string(key.data)] = decoded.AsInterface()
	}
	call.Arguments, err = json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("cursor: invalid MCP arguments")
	}
	if call.CallID == "" || call.ExecID == "" {
		return nil, fmt.Errorf("cursor: missing MCP execution identity")
	}
	return call, nil
}

func encodeToolReply(call ToolCall, result ToolResult, failure string) message {
	var outcome, body, exec, client message
	if failure != "" {
		body.text(1, failure)
		outcome.bytes(2, body)
	} else {
		for _, text := range result.Text {
			var content, item message
			content.text(1, text)
			item.bytes(1, content)
			body.bytes(1, item)
		}
		if result.IsError {
			body.number(2, 1)
		}
		outcome.bytes(1, body)
	}
	exec.number(1, uint64(call.ID))
	exec.text(15, call.ExecID)
	exec.bytes(11, outcome)
	client.bytes(2, exec)
	return client
}
