package main

import (
	"bytes"
	"testing"
)

func TestRetireCommandUsage(t *testing.T) {
	t.Setenv("OMNIHIVE_REFACTOR_DSN", "")
	for _, test := range []struct {
		args []string
		code int
	}{
		{[]string{"--help"}, 0}, {[]string{"report", "--help"}, 0},
		{nil, 2}, {[]string{"apply"}, 2}, {[]string{"plan", "--dry-run=false"}, 2},
		{[]string{"report", "--format=csv"}, 2}, {[]string{"scan"}, 2},
	} {
		var out, diag bytes.Buffer
		if got := run(t.Context(), test.args, &out, &diag); got != test.code {
			t.Errorf("%q returned %d, expected %d", test.args, got, test.code)
		}
	}
}
