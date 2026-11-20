package refactor

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunArguments(t *testing.T) {
	t.Setenv("OMNIHIVE_REFACTOR_DSN", "")
	for _, tc := range []struct {
		name       string
		args       []string
		code       int
		wantOutput string
		wantError  string
	}{
		{name: "root help", args: []string{"--help"}, wantOutput: usage},
		{name: "status help", args: []string{"status", "--help"}, wantOutput: usage},
		{name: "missing command", code: 2, wantError: usage},
		{name: "future command", args: []string{"cutover"}, code: 2, wantError: usage},
		{name: "future workspace flag", args: []string{"inventory", "--workspace=123"}, code: 2, wantError: usage},
		{name: "unsupported format", args: []string{"inventory", "--format=xml"}, code: 2, wantError: usage},
		{name: "trailing argument", args: []string{"status", "--format=json", "sentinel_credential"}, code: 2, wantError: usage},
		{name: "missing database", args: []string{"inventory", "--format=json"}, code: 2, wantError: "OMNIHIVE_REFACTOR_DSN is required"},
		{name: "default format", args: []string{"status"}, code: 2, wantError: "OMNIHIVE_REFACTOR_DSN is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), tc.args, &stdout, &stderr)
			require.Equal(t, tc.code, code)
			checkText(t, tc.wantOutput, stdout.String())
			checkText(t, tc.wantError, stderr.String())
		})
	}
}

func TestRunRedactsConnectionConfigurationErrors(t *testing.T) {
	for _, tc := range []struct {
		name, app, restore, message string
	}{
		{"app", "password=sentinel_credential invalid_option", "", "Could not configure application database connection"},
		{"restore", "host=127.0.0.1", "password=sentinel_credential invalid_option", "Could not configure restore journal database connection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OMNIHIVE_REFACTOR_DSN", tc.app)
			t.Setenv("OMNIHIVE_RESTORE_JOURNAL_DSN", tc.restore)
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"inventory", "--format=json"}, &stdout, &stderr)
			require.Equal(t, 1, code)
			require.Empty(t, stdout.String())
			checkText(t, tc.message, stderr.String())
		})
	}
}

func checkText(t *testing.T, want, got string) {
	t.Helper()
	if want != "" {
		want += "\n"
	}
	require.Equal(t, want, got)
}
