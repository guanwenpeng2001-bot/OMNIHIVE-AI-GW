package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/tidwall/gjson"
)

func stableAffinityIdentity(parsed *ParsedRequest) string {
	payload := affinityPrefixPayload(parsed)
	if len(payload) == 0 {
		return ""
	}
	scope := parsed.SessionContext
	if len(scope.AffinityHMACKey) > 0 || scope.AffinityHMACKeyVersion != "" {
		return hmacAffinityIdentity(payload, scope.AffinityHMACKey, scope.AffinityHMACKeyVersion)
	}
	// Existing deployments can migrate without reading any legacy binding.
	// Configured HMAC and unkeyed transition mode must not share a namespace.
	digest := sha256.Sum256(payload)
	return "v2:sha256:" + hex.EncodeToString(digest[:])
}

// hmacAffinityIdentity is deterministic across instances sharing the key and
// its version. Version framing prevents ambiguous rotation namespaces; neither
// plaintext prompts nor key material appear in the returned routing key.
func hmacAffinityIdentity(payload, key []byte, version string) string {
	if len(payload) == 0 || len(key) == 0 || version == "" {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	writeAffinityField(mac, "key-version", []byte(version))
	writeAffinityField(mac, "payload", payload)
	return "v2:hmac:" + base64.RawURLEncoding.EncodeToString([]byte(version)) + ":" + hex.EncodeToString(mac.Sum(nil))
}

// affinityPrefixPayload is routing identity only. It must never be used as a
// key for mutable tool/reasoning state: independent conversations can share a
// first user message and therefore intentionally share routing affinity.
func affinityPrefixPayload(parsed *ParsedRequest) []byte {
	if parsed == nil || parsed.SessionContext == nil {
		return nil
	}
	scope := parsed.SessionContext
	if scope.APIKeyID <= 0 || scope.OwnerID <= 0 {
		return nil
	}
	anchor := firstAffinityUserContent(parsed)
	if len(anchor) == 0 {
		return nil
	}
	var payload bytes.Buffer
	writeAffinityField(&payload, "identity", []byte("routing-affinity-v2"))
	writeAffinityField(&payload, "owner", []byte(strconv.FormatInt(scope.OwnerID, 10)))
	writeAffinityField(&payload, "api-key", []byte(strconv.FormatInt(scope.APIKeyID, 10)))
	writeAffinityField(&payload, "route", []byte(scope.RouteScope))
	group := "none"
	if parsed.GroupID != nil {
		group = strconv.FormatInt(*parsed.GroupID, 10)
	}
	writeAffinityField(&payload, "group", []byte(group))
	writeAffinityField(&payload, "protocol", []byte(parsed.protocol))
	writeAffinityField(&payload, "model", []byte(parsed.Model))
	writeAffinityField(&payload, "system", canonicalAffinityJSON(parsed.SystemRaw()))
	writeAffinityField(&payload, "first-user", anchor)
	return payload.Bytes()
}

// writeAffinityField frames both type and value lengths. Delimiters appearing
// in prompts, models or route names cannot move data across field boundaries.
func writeAffinityField(w io.Writer, kind string, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(kind)))
	_, _ = w.Write(size[:])
	_, _ = io.WriteString(w, kind)
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = w.Write(size[:])
	_, _ = w.Write(value)
}

// canonicalAffinityJSON preserves content types, block boundaries and media,
// while ignoring JSON whitespace and object key order. UseNumber avoids losing
// integer precision in structured content.
func canonicalAffinityJSON(raw []byte) []byte {
	if len(raw) == 0 {
		return []byte("null")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return nil
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return canonical
}

// firstAffinityUserContent deliberately stops at the first user message, even
// if empty: appending a later turn must not invent a different initial anchor.
func firstAffinityUserContent(parsed *ParsedRequest) []byte {
	var anchor []byte
	messages := parseRawJSONView(parsed.MessagesRaw())
	if parsed.protocol == "responses" {
		messages = parseRawJSONView(parsed.InputRaw())
		if messages.Type == gjson.String {
			if hasAffinityContent(messages) {
				return canonicalAffinityJSON([]byte(messages.Raw))
			}
			return nil
		}
	}
	if !messages.IsArray() {
		return nil
	}
	messages.ForEach(func(_, message gjson.Result) bool {
		role := message.Get("role").String()
		implicitUser := role == "" && (parsed.protocol == domain.PlatformGemini ||
			(parsed.protocol == "responses" && (message.Get("type").String() == "" || message.Get("type").String() == "message")))
		if role != "user" && !implicitUser {
			return true
		}
		content := message.Get("content")
		if parsed.protocol == domain.PlatformGemini {
			content = message.Get("parts")
		}
		if hasAffinityContent(content) {
			anchor = canonicalAffinityJSON([]byte(content.Raw))
		}
		return false
	})
	return anchor
}

func hasAffinityContent(content gjson.Result) bool {
	if content.Type == gjson.String {
		return strings.TrimSpace(content.String()) != ""
	}
	if content.IsArray() {
		found := false
		content.ForEach(func(_, part gjson.Result) bool {
			found = hasAffinityContent(part)
			return !found
		})
		return found
	}
	if content.IsObject() {
		// A text block's type label alone is not a conversation anchor.
		if text := content.Get("text"); text.Exists() {
			return text.Type == gjson.String && strings.TrimSpace(text.String()) != ""
		}
		for _, field := range []string{"source", "inline_data", "inlineData", "file_data", "fileData", "image_url", "input_audio", "file_id"} {
			value := content.Get(field)
			if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
				return true
			}
			if value.IsObject() && len(value.Map()) > 0 {
				return true
			}
		}
		return hasAffinityContent(content.Get("content"))
	}
	return false
}
