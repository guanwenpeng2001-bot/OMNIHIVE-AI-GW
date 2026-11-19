// Test guest built for wasip1 by the host integration test.
package main

import (
	"bytes"
	"os"
	"strings"

	v2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2"
)

var scopes = "provider-ui"

func main() {
	caps := []string{v2.Provider, v2.UI}
	if scopes == "task" {
		caps = []string{v2.Task}
	}
	if err := v2.Serve("com.example.openai-transport", "0.1.0", caps, func(r *v2.Request) (*v2.Reply, error) {
		if r.Operation == "authorize" {
			switch string(r.ConfigJson) {
			case `{"mode":"trap"}`:
				panic("fixture")
			case `{"mode":"timeout"}`:
				for {
				}
			case `{"mode":"memory"}`:
				_ = bytes.Repeat([]byte{42}, 96<<20)
			case `{"mode":"output"}`:
				_, _ = os.Stdout.WriteString(strings.Repeat("x", v2.MaxFrame+1))
			}
		}
		return &v2.Reply{Accepted: true, NormalizedConfigJson: r.ConfigJson}, nil
	}); err != nil {
		os.Exit(1)
	}
}
