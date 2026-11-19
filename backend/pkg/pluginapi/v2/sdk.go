// Package pluginv2 defines the local OmniHive WASI extension contract.
package pluginv2

import (
	"errors"
	"io"
	"os"

	"google.golang.org/protobuf/proto"
)

const (
	ProductID       = "omnihive"
	Handshake       = "omnihive.plugin.v2"
	ProtocolVersion = 2
	RuntimeKey      = "wasip1-wasm"
	Extension       = ".ohplugin"
	Provider        = "provider"
	Task            = "task"
	UI              = "ui"
	MaxFrame        = 256 << 10
	MaxModule       = 32 << 20
)

// Origin records the immutable input to an offline v1 rebuild.
type Origin struct {
	PluginID       string `json:"plugin_id"`
	KeyID          string `json:"key_id"`
	ArchiveSHA256  string `json:"archive_sha256"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

// Serve handles one request per WASI instance. No sockets or files are preopened.
func Serve(id, version string, capabilities []string, handle func(*Request) (*Reply, error)) error {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, MaxFrame+1))
	if err != nil || len(raw) > MaxFrame {
		return errors.New("invalid request size")
	}
	var request Request
	if err := proto.Unmarshal(raw, &request); err != nil || request.Handshake != Handshake {
		return errors.New("invalid handshake")
	}
	reply, err := handle(&request)
	if err != nil {
		return err
	}
	if reply == nil {
		return errors.New("missing extension reply")
	}
	reply.Handshake, reply.PluginId, reply.Version, reply.Capabilities = Handshake, id, version, capabilities
	raw, err = proto.Marshal(reply)
	if err != nil || len(raw) > MaxFrame {
		return errors.New("invalid reply size")
	}
	_, err = os.Stdout.Write(raw)
	return err
}
