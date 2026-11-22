//go:build unit

package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func newObservedLogger(t *testing.T) (*zap.Logger, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.WarnLevel)
	return zap.New(core), logs
}

func loggedFields(t *testing.T, logs *observer.ObservedLogs) map[string]any {
	t.Helper()
	entries := logs.All()
	require.Len(t, entries, 1)
	fields := map[string]any{}
	for _, f := range entries[0].Context {
		switch f.Key {
		case "body_len":
			fields[f.Key] = int(f.Integer)
		case "error":
			fields[f.Key] = f.String
		default:
			fields[f.Key] = f.String
		}
	}
	return fields
}

func TestLogRequestBodyParseFailure_DerivesErrorWhenNil(t *testing.T) {
	log, logs := newObservedLogger(t)
	body := []byte(`{"model": bad}`)

	logRequestBodyParseFailure(log, body, nil)

	fields := loggedFields(t, logs)
	require.Equal(t, len(body), fields["body_len"])
	require.Contains(t, fields["error"], "invalid json")
	require.Contains(t, fields["error"], "offset=11")
}

func TestLogRequestBodyParseFailure_ShortBodyHasNoTail(t *testing.T) {
	log, logs := newObservedLogger(t)
	body := []byte(`{"broken":`)

	logRequestBodyParseFailure(log, body, nil)

	fields := loggedFields(t, logs)
	require.Contains(t, fields, "body_head")
	require.NotContains(t, fields, "body_tail")
	require.Contains(t, fields["body_head"].(string), `{\"broken\":`)
}

func TestLogRequestBodyParseFailure_LargeBodyBoundedSnippets(t *testing.T) {
	log, logs := newObservedLogger(t)
	// A large body within the redaction limit retains bounded head and tail snippets,
	// and neither snippet may exceed the configured bound (plus quoting overhead).
	body := []byte(`{"model":"claude-sonnet-4-6","big":"` + strings.Repeat("A", 32<<10) + `"`)

	logRequestBodyParseFailure(log, body, nil)

	fields := loggedFields(t, logs)
	require.Equal(t, len(body), fields["body_len"])
	require.IsType(t, "", fields["body_head"])
	require.IsType(t, "", fields["body_tail"])
	head := fields["body_head"].(string)
	tail := fields["body_tail"].(string)
	require.Contains(t, head, "claude-sonnet-4-6")
	require.Contains(t, tail, "AAA")
	require.NotContains(t, tail, "claude-sonnet-4-6")
	// strconv.Quote adds surrounding quotes and escapes; 4x is a generous cap.
	require.LessOrEqual(t, len(head), parseFailureSnippetLen*4)
	require.LessOrEqual(t, len(tail), parseFailureSnippetLen*4)
}

func TestLogRequestBodyParseFailure_OversizedBodyIsRedacted(t *testing.T) {
	log, logs := newObservedLogger(t)
	body := []byte(`{"authorization":"Bearer sensitive-value","big":"` + strings.Repeat("A", 1<<20) + `"`)

	logRequestBodyParseFailure(log, body, nil)

	fields := loggedFields(t, logs)
	require.Equal(t, len(body), fields["body_len"])
	require.Equal(t, `"<payload size limit exceeded>"`, fields["body_head"])
	require.NotContains(t, fields, "body_tail")
	require.NotContains(t, fields["error"], "sensitive-value")
}

func TestLogRequestBodyParseFailure_EscapesControlCharacters(t *testing.T) {
	log, logs := newObservedLogger(t)
	body := []byte("{\"model\":\x01\n\"x\"}")

	logRequestBodyParseFailure(log, body, nil)

	fields := loggedFields(t, logs)
	head := fields["body_head"].(string)
	require.NotContains(t, head, "\n")
	require.NotContains(t, head, "\x01")
	require.Contains(t, head, `\n`)
	require.Contains(t, head, `\x01`)
}

func TestLogRequestBodyParseFailure_NilLoggerNoPanic(t *testing.T) {
	require.NotPanics(t, func() {
		logRequestBodyParseFailure(nil, []byte(`{`), nil)
	})
}

func TestLogRequestBodyParseFailureRedactsBeforeTruncation(t *testing.T) {
	log, logs := newObservedLogger(t)
	body := []byte(`{"Authorization":"Bearer ` + strings.Repeat("credential-secret", 1000) + `","api_key":"credential-secret",`)
	logRequestBodyParseFailure(log, body, nil)
	fields := loggedFields(t, logs)
	for _, name := range []string{"body_head", "body_tail", "error"} {
		if value, ok := fields[name].(string); ok {
			require.NotContains(t, value, "credential-secret")
			require.NotContains(t, value, "Bearer")
		}
	}
}
