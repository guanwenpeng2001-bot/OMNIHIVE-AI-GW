package service

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const zenFreeUserAgent = "opencode/1.18.34"

var zenCanonicalRequest = regexp.MustCompile(`^msg_[0-9a-f]{12}[0-9A-Za-z]{14}$`)
var zenCanonicalSession = regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`)
var zenCoreTools = []string{"bash", "edit", "glob", "grep", "read"}

// Match zen-free/provider.mjs: SHA256("ses\\0" + signal), first six
// bytes as hex, the next ten encoded as a fourteen-character base62 suffix.
func zenCanonicalID(prefix, signal string) string {
	sum := sha256.Sum256([]byte(prefix + "\x00" + signal))
	value := new(big.Int).SetBytes(sum[6:16])
	radix := big.NewInt(62)
	rem := new(big.Int)
	suffix := make([]byte, 14)
	alphabet := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	for i := 13; i >= 0; i-- {
		value.QuoRem(value, radix, rem)
		suffix[i] = alphabet[rem.Int64()]
	}
	return prefix + "_" + hex.EncodeToString(sum[:6]) + string(suffix)
}
func zenRandomID(prefix string) string {
	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		panic(err)
	}
	return zenCanonicalID(prefix, hex.EncodeToString(seed[:]))
}
func zenSession(c *gin.Context, h http.Header, bodies ...[]byte) string {
	signal := ""
	for _, name := range []string{"X-OpenCode-Session", "X-Session-Affinity", "X-Session-Id", "Conversation-Id"} {
		if c != nil && c.Request != nil {
			signal = strings.TrimSpace(c.GetHeader(name))
		}
		if signal != "" {
			break
		}
	}
	if signal == "" {
		for _, body := range bodies {
			signal = gjson.GetBytes(body, "metadata.session_id").String()
			if signal != "" {
				break
			}
		}
	}
	if signal == "" {
		// The builder may already have recovered caller affinity before conversion.
		for _, name := range []string{"X-OpenCode-Session", "X-Session-Affinity", "X-Session-Id", "Conversation-Id"} {
			signal = strings.TrimSpace(h.Get(name))
			if signal != "" {
				break
			}
		}
	}
	if signal == "" {
		for _, body := range bodies {
			messages := gjson.GetBytes(body, "messages")
			if !messages.Exists() {
				messages = gjson.GetBytes(body, "input")
			}
			if messages.IsArray() {
				for _, m := range messages.Array() {
					if m.Get("role").String() == "user" {
						signal = zenSessionContent(m.Get("content"))
						break
					}
				}
			} else if messages.Type == gjson.String {
				signal = zenSessionContent(messages)
			}
			if signal != "" {
				break
			}
		}
	}
	if signal == "" {
		return zenRandomID("ses")
	}
	if zenCanonicalSession.MatchString(signal) {
		return signal
	}
	return zenCanonicalID("ses", signal)
}
func applyZenFreeDiscoveryHeaders(h http.Header) {
	deleteFreeModelHeader(h, "Authorization")
	deleteFreeModelHeader(h, "X-API-Key")
	h.Set("Authorization", "Bearer public")
	deleteFreeModelHeader(h, "User-Agent")
	deleteFreeModelHeader(h, "X-OpenCode-Client")
	h.Set("User-Agent", zenFreeUserAgent)
	h.Set("X-OpenCode-Client", "cli")
	h.Set("Accept", "application/json")
}
func applyZenFreeHeaders(c *gin.Context, h http.Header, anthropic bool, bodies ...[]byte) {
	session := zenSession(c, h, bodies...)
	deleteFreeModelHeader(h, "Authorization")
	deleteFreeModelHeader(h, "X-API-Key")
	deleteFreeModelHeader(h, "Anthropic-Version")
	if anthropic {
		h.Set("X-API-Key", "public")
		h.Set("Anthropic-Version", "2023-06-01")
	} else {
		h.Set("Authorization", "Bearer public")
	}
	deleteFreeModelHeader(h, "User-Agent")
	deleteFreeModelHeader(h, "X-OpenCode-Client")
	h.Set("User-Agent", zenFreeUserAgent)
	h.Set("X-OpenCode-Client", "cli")
	project := h.Get("X-OpenCode-Project")
	deleteFreeModelHeader(h, "X-OpenCode-Project")
	if project == "" {
		project = "global"
	}
	h.Set("X-OpenCode-Project", project)
	requestID := h.Get("X-OpenCode-Request")
	deleteFreeModelHeader(h, "X-OpenCode-Request")
	if !zenCanonicalRequest.MatchString(requestID) {
		requestID = zenRandomID("msg")
	}
	h.Set("X-OpenCode-Request", requestID)
	for _, name := range []string{"X-OpenCode-Session", "X-Session-Id", "X-Session-Affinity"} {
		deleteFreeModelHeader(h, name)
		h.Set(name, session)
	}
}

// Discovery is authoritative for this identity. Configuration and availability
// may select a model, but cannot expand the free lane's discovered membership.
func zenFreeModelAllowed(account *Account, model string) bool {
	if !account.IsOpenCodeZenFree() {
		return true
	}
	if !safeFreeModelID(model) || zenDecisionName.MatchString(normalizeOpenCodeGoModelID(model)) {
		return false
	}
	metadata, hasMetadata := account.GetUpstreamModelMetadata(model)
	if hasMetadata {
		if metadata.Free != nil && !*metadata.Free {
			return false
		}
		for _, method := range metadata.SupportedMethods {
			if strings.EqualFold(method, "systemone") {
				return false
			}
		}
		if len(metadata.Pricing) > 0 {
			raw, err := json.Marshal(metadata.Pricing)
			if err != nil {
				return false
			}
			var pricing any
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			if decoder.Decode(&pricing) != nil {
				return false
			}
			if _, zero := zenCostVerdict(pricing, 0); !zero {
				return false
			}
		}
	}
	if catalog := account.GetUpstreamModelCatalog(); catalog != nil && catalog.SourceKind != modelEvidenceLegacyMappingUpgrade && catalog.SourceKind != ModelEvidenceConfiguredSelection && catalog.SourceKind != ModelEvidenceConfiguredCompactSelection {
		return modelCatalogIdentityMatches(account, nil, catalog) && slices.Contains(catalog.DiscoveredModels, model) && metadata.Free != nil && *metadata.Free
	}
	for _, preset := range openCodeZenFreeModelPresets() {
		for _, candidate := range preset.Models {
			if candidate.ID == model {
				return true
			}
		}
	}
	return false
}

type zenFreeToolPolicy struct {
	protocol      string
	renamed       map[string]string
	injected      map[string]bool
	pending       map[string]map[int]map[string]any
	dropped, real map[string]bool
	blocks        map[string]bool
	bytes         int
	legacy        bool
	indexes       map[string]int
	nextIndex     int
}

func newZenFreeToolPolicy(protocol string) *zenFreeToolPolicy {
	return &zenFreeToolPolicy{protocol: protocol, renamed: map[string]string{}, injected: map[string]bool{}, pending: map[string]map[int]map[string]any{}, dropped: map[string]bool{}, real: map[string]bool{}, blocks: map[string]bool{}, indexes: map[string]int{}}
}
func freeObject(v any) map[string]any { m, _ := v.(map[string]any); return m }
func freeArray(v any) []any           { a, _ := v.([]any); return a }
func freeString(v any) string         { s, _ := v.(string); return s }
func zenToolDefinition(tool any) map[string]any {
	t := freeObject(tool)
	if f := freeObject(t["function"]); f != nil {
		return f
	}
	return t
}
func prepareZenFreeRequest(account *Account, body []byte, protocol string) ([]byte, *zenFreeToolPolicy, error) {
	b, err := decodeFreeModelObject(body)
	if err != nil {
		return nil, nil, err
	}
	if !zenFreeModelAllowed(account, freeString(b["model"])) {
		return nil, nil, fmt.Errorf("%w: model is not in Zen's free pool", ErrAccountModelUnsupported)
	}
	p := newZenFreeToolPolicy(protocol)
	if v, ok := b["tools"]; ok && v != nil {
		if _, ok := v.([]any); !ok {
			return nil, nil, errors.New("tools must be an array")
		}
	}
	tools := freeArray(b["tools"])
	// Mirror legacy functions into the tool fingerprint and restore the legacy
	// response shape for callers that supplied only functions.
	p.legacy = protocol == APIProtocolChatCompletions && len(tools) == 0 && len(freeArray(b["functions"])) > 0
	if p.legacy {
		b["parallel_tool_calls"] = false
	}
	definitions := []map[string]any{}
	for _, t := range tools {
		definitions = append(definitions, zenToolDefinition(t))
	}
	for _, t := range freeArray(b["functions"]) {
		definitions = append(definitions, freeObject(t))
		tools = append(tools, map[string]any{"type": "function", "function": t})
	}
	present := map[string]bool{}
	reverse := map[string]string{}
	for _, d := range definitions {
		name := freeString(d["name"])
		canonical := strings.ToLower(name)
		if !slices.Contains(zenCoreTools, canonical) {
			continue
		}
		if present[canonical] {
			return nil, nil, fmt.Errorf("ambiguous Zen Free core tool %q", canonical)
		}
		present[canonical] = true
		if name != canonical {
			p.renamed[canonical] = name
			reverse[name] = canonical
			d["name"] = canonical
		}
	}
	if !present["bash"] {
		for _, d := range definitions {
			name := freeString(d["name"])
			if strings.EqualFold(name, "pwsh") {
				d["name"] = "bash"
				p.renamed["bash"] = name
				reverse[name] = "bash"
				present["bash"] = true
				break
			}
		}
	}
	// Rewrite only tool references, never argument objects or user text.
	renameRef := func(d map[string]any) {
		if name := freeString(d["name"]); reverse[name] != "" {
			d["name"] = reverse[name]
		}
	}
	for _, key := range []string{"tool_choice", "function_call"} {
		if d := freeObject(b[key]); d != nil {
			renameRef(zenToolDefinition(d))
		}
	}
	for _, key := range []string{"messages", "input"} {
		for _, raw := range freeArray(b[key]) {
			m := freeObject(raw)
			if m == nil {
				continue
			}
			if m["role"] == "function" {
				renameRef(m)
			}
			if m["type"] == "function_call" {
				renameRef(m)
			}
			if d := freeObject(m["function_call"]); d != nil {
				renameRef(d)
			}
			for _, t := range freeArray(m["tool_calls"]) {
				renameRef(zenToolDefinition(t))
			}
			for _, t := range freeArray(m["content"]) {
				d := freeObject(t)
				if d["type"] == "tool_use" {
					renameRef(d)
				}
			}
		}
	}
	for _, name := range zenCoreTools {
		if present[name] {
			continue
		}
		p.injected[name] = true
		schema := map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
		d := map[string]any{"name": name, "description": "Not available. Never call this tool."}
		switch protocol {
		case APIProtocolAnthropic:
			d["input_schema"] = schema
			tools = append(tools, d)
		case APIProtocolResponses:
			d["type"] = "function"
			d["parameters"] = schema
			tools = append(tools, d)
		default:
			d["parameters"] = schema
			tools = append(tools, map[string]any{"type": "function", "function": d})
		}
	}
	b["tools"] = tools
	out, err := json.Marshal(b)
	return out, p, err
}
func (p *zenFreeToolPolicy) call(d map[string]any, key string) bool {
	name := freeString(d["name"])
	if p.injected[strings.ToLower(name)] {
		p.dropped[key] = true
		return false
	}
	if original := p.renamed[name]; original != "" {
		d["name"] = original
	}
	p.real[key] = true
	return true
}
func (p *zenFreeToolPolicy) chatMessage(m map[string]any, key string) {
	if m == nil {
		return
	}
	if raw, ok := m["tool_calls"]; ok {
		kept := []any{}
		for _, t := range freeArray(raw) {
			if p.call(zenToolDefinition(t), key) {
				kept = append(kept, t)
			}
		}
		if len(kept) > 0 {
			if p.legacy {
				m["function_call"] = zenToolDefinition(kept[0])
				delete(m, "tool_calls")
			} else {
				m["tool_calls"] = kept
			}
		} else {
			delete(m, "tool_calls")
		}
	}
	if d := freeObject(m["function_call"]); d != nil && !p.call(d, key) {
		delete(m, "function_call")
	}
}
func freeIndex(v any) int { n, _ := v.(json.Number); i, _ := n.Int64(); return int(i) }
func (p *zenFreeToolPolicy) chat(b map[string]any) error {
	for _, raw := range freeArray(b["choices"]) {
		c := freeObject(raw)
		key := fmt.Sprint(c["index"])
		p.chatMessage(freeObject(c["message"]), key)
		delta := freeObject(c["delta"])
		if delta != nil {
			for _, t := range freeArray(delta["tool_calls"]) {
				if err := p.bufferTool(key, freeIndex(freeObject(t)["index"]), freeObject(t)); err != nil {
					return err
				}
			}
			if f := freeObject(delta["function_call"]); f != nil {
				if err := p.bufferTool(key, -1, map[string]any{"function": f}); err != nil {
					return err
				}
			}
			delete(delta, "tool_calls")
			delete(delta, "function_call")
		}
		if c["finish_reason"] != nil {
			if pending := p.pending[key]; len(pending) > 0 {
				if delta == nil {
					delta = map[string]any{}
					c["delta"] = delta
				}
				indexes := []int{}
				for i := range pending {
					indexes = append(indexes, i)
				}
				sort.Ints(indexes)
				tools := []any{}
				for _, i := range indexes {
					t := pending[i]
					if !p.call(zenToolDefinition(t), key) {
						continue
					}
					if i == -1 || p.legacy {
						delta["function_call"] = t["function"]
					} else {
						t["index"] = len(tools)
						tools = append(tools, t)
					}
				}
				if len(tools) > 0 {
					delta["tool_calls"] = tools
				}
				delete(p.pending, key)
			}
			if p.legacy && p.real[key] && c["finish_reason"] == "tool_calls" {
				c["finish_reason"] = "function_call"
			}
			if p.dropped[key] && !p.real[key] && (c["finish_reason"] == "tool_calls" || c["finish_reason"] == "function_call") {
				c["finish_reason"] = "stop"
			}
		}
	}
	return nil
}
func (p *zenFreeToolPolicy) bufferTool(key string, index int, t map[string]any) error {
	encoded, _ := json.Marshal(t)
	p.bytes += len(encoded)
	if p.bytes > freeModelBodyLimit {
		return errors.New("zen free pending tools exceed size limit")
	}
	if p.pending[key] == nil {
		p.pending[key] = map[int]map[string]any{}
	}
	dest := p.pending[key][index]
	if dest == nil {
		dest = map[string]any{"function": map[string]any{}}
		p.pending[key][index] = dest
	}
	for k, v := range t {
		if k != "function" {
			dest[k] = v
		}
	}
	fn := freeObject(t["function"])
	out := freeObject(dest["function"])
	for k, v := range fn {
		if k == "name" || k == "arguments" {
			out[k] = freeString(out[k]) + freeString(v)
		} else {
			out[k] = v
		}
	}
	return nil
}
func (p *zenFreeToolPolicy) snapshot(b map[string]any) {
	for _, key := range []string{"output", "content"} {
		if list, ok := b[key].([]any); ok {
			kept := []any{}
			for _, item := range list {
				m := freeObject(item)
				if m != nil {
					if m["type"] == "function_call" || m["type"] == "tool_use" {
						if !p.call(m, "message") {
							continue
						}
					}
					p.snapshot(m)
				}
				kept = append(kept, item)
			}
			b[key] = kept
		}
	}
	for _, key := range []string{"response", "message"} {
		if m := freeObject(b[key]); m != nil {
			p.snapshot(m)
		}
	}
	if p.dropped["message"] && !p.real["message"] && b["stop_reason"] == "tool_use" {
		b["stop_reason"] = "end_turn"
	}
}
func (p *zenFreeToolPolicy) guard(body []byte) ([]byte, error) {
	if strings.TrimSpace(string(body)) == "[DONE]" {
		if len(p.pending) > 0 {
			return nil, errors.New("zen free tool stream ended before finish_reason")
		}
		return body, nil
	}
	b, err := decodeFreeModelObject(body)
	if err != nil {
		return nil, err
	}
	if p.protocol == APIProtocolChatCompletions {
		if err := p.chat(b); err != nil {
			return nil, err
		}
	} else {
		typ := freeString(b["type"])
		if p.protocol == APIProtocolAnthropic {
			key := fmt.Sprint(b["index"])
			if typ == "content_block_start" {
				d := freeObject(b["content_block"])
				p.blocks[key] = d["type"] == "tool_use" && !p.call(d, "message")
			}
			if strings.HasPrefix(typ, "content_block_") && p.blocks[key] {
				return nil, nil
			}
			if b["index"] != nil {
				p.remapIndex(b, "index", key)
			}
			if d := freeObject(b["delta"]); d != nil && d["stop_reason"] == "tool_use" && p.dropped["message"] && !p.real["message"] {
				d["stop_reason"] = "end_turn"
			}
		} else {
			key := fmt.Sprint(b["output_index"])
			item := freeObject(b["item"])
			if item["type"] == "function_call" {
				dropped := !p.call(item, "message")
				p.blocks[key] = dropped
				if id := freeString(item["id"]); id != "" {
					p.blocks[id] = dropped
				}
			}
			if b["output_index"] != nil && p.blocks[key] || p.blocks[freeString(b["item_id"])] {
				return nil, nil
			}
			if b["output_index"] != nil {
				p.remapIndex(b, "output_index", key)
			}
		}
		p.snapshot(b)
	}
	return json.Marshal(b)
}

func (p *zenFreeToolPolicy) remapIndex(b map[string]any, field, key string) {
	index, exists := p.indexes[key]
	if !exists {
		index = p.nextIndex
		p.nextIndex++
		p.indexes[key] = index
	}
	b[field] = index
}

func zenSessionContent(value gjson.Result) string {
	if value.Type == gjson.String {
		encoded, _ := json.Marshal(value.String())
		return string(encoded)
	}
	var out bytes.Buffer
	if json.Compact(&out, []byte(value.Raw)) == nil {
		return out.String()
	}
	return ""
}
