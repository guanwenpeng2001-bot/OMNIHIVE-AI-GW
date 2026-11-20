// Build with GOOS=wasip1 GOARCH=wasm go build -o plugin.wasm .
package main

import (
	"os"

	plugin "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2"
)

func main() {
	err := plugin.Serve("com.example.openai-transport", "0.1.0", []string{plugin.Provider, plugin.UI}, func(req *plugin.Request) (*plugin.Reply, error) {
		reply := &plugin.Reply{}
		switch req.Operation {
		case "start", "test":
			reply.Accepted = true
		case "configure":
			reply.Accepted = true
			reply.NormalizedConfigJson = req.ConfigJson
		case "authorize":
			reply.Accepted = req.Capability == plugin.Provider
		}
		return reply, nil
	})
	if err != nil {
		os.Exit(1)
	}
}
