package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sort"
	"sync/atomic"
	"time"

	v1 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v1"
	v2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// Compiled modules share code, never guest memory, file descriptors or globals.
type extensionRuntime struct {
	vm        wazero.Runtime
	module    wazero.CompiledModule
	installer *PluginPackageInstaller
	manifest  PluginManifest
	key       ed25519.PublicKey
	caps      []string
	config    atomic.Pointer[[]byte]
	closed    atomic.Bool
}

var extensionSlots = make(chan struct{}, 8)

func (m *PluginManager) startExtension(ctx context.Context, installation *PluginInstallation, timeout time.Duration) (*pluginRuntime, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	data, err := m.repo.GetArtifact(ctx, installation.ID)
	if err != nil {
		return nil, err
	}
	ar, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("missing or invalid signed v2 archive")
	}
	signed, raw, _, err := m.installer.inspectArchive(ar)
	if err != nil {
		return nil, err
	}
	copy := *installation
	copy.Manifest = signed
	if signed.SchemaVersion != 2 || signed.ID != installation.PluginKey || signed.Version != installation.Version || !samePluginPackage(&copy, installation) {
		return nil, errors.New("signed v2 manifest differs from installed record")
	}
	var sig, mod *zip.File
	modulePath := signed.Runtimes[v2.RuntimeKey].Path
	for _, entry := range ar.File {
		if entry.Name == pluginSignatureFilename {
			sig = entry
		}
		if entry.Name == modulePath {
			mod = entry
		}
	}
	key, err := m.installer.verifyExtensionSignature(sig, raw, signed)
	if err != nil {
		return nil, err
	}
	if mod == nil {
		return nil, errors.New("missing WASI module")
	}
	binary, err := readPluginZipFile(mod, v2.MaxModule)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(binary)
	if hex.EncodeToString(digest[:]) != signed.Files[modulePath] || signed.Files[modulePath] != installation.BinarySHA256 {
		return nil, errors.New("WASI module digest mismatch")
	}
	vm := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true).WithMemoryLimitPages(1024))
	ext := &extensionRuntime{vm: vm, manifest: signed, installer: m.installer, key: key}
	success := false
	defer func() {
		if !success {
			ext.close()
		}
	}()
	if _, err = wasi_snapshot_preview1.Instantiate(ctx, vm); err != nil {
		return nil, err
	}
	ext.module, err = vm.CompileModule(ctx, binary)
	if err != nil {
		return nil, errors.New("invalid WASI module or memory limit exceeded")
	}
	for _, fn := range ext.module.ImportedFunctions() {
		ns, _, _ := fn.Import()
		if ns != wasi_snapshot_preview1.ModuleName {
			return nil, extensionDenied(signed.ID, "import", "only WASI Preview 1 imports are available")
		}
	}
	if len(ext.module.ImportedMemories()) > 0 {
		return nil, extensionDenied(signed.ID, "import", "imported host memory is forbidden")
	}
	for _, cap := range signed.SortedCapabilities() {
		ext.caps = append(ext.caps, cap.ID)
	}
	if _, err = ext.exchange(ctx, "start", "", nil); err != nil {
		return nil, err
	}
	success = true
	return &pluginRuntime{installation: installation, extension: ext, api: &extensionConfigClient{ext: ext}, done: make(chan struct{})}, nil
}

func (e *extensionRuntime) close() {
	if !e.closed.Swap(true) {
		_ = e.vm.Close(context.Background())
	}
}

func (e *extensionRuntime) trusted() error {
	key, err := e.installer.extensionPublisher(e.manifest)
	if err != nil {
		return err
	}
	if !bytes.Equal(key, e.key) {
		return extensionDenied(e.manifest.ID, "trust", "verified signing key has changed")
	}
	return nil
}

type boundedExtensionOutput struct {
	bytes.Buffer
	exceeded bool
}

func (w *boundedExtensionOutput) Write(p []byte) (int, error) {
	if len(p) > v2.MaxFrame-w.Len() {
		w.exceeded = true
		return 0, io.ErrShortBuffer
	}
	return w.Buffer.Write(p)
}

func (e *extensionRuntime) exchange(ctx context.Context, operation, cap string, config []byte) (*v2.Reply, error) {
	if e.closed.Load() {
		return nil, errors.New("extension is disabled")
	}
	if err := e.trusted(); err != nil {
		return nil, err
	}
	select {
	case extensionSlots <- struct{}{}:
		defer func() { <-extensionSlots }()
	default:
		return nil, extensionDenied(e.manifest.ID, cap, "host concurrent invocation limit")
	}
	if len(config) > v2.MaxFrame {
		return nil, errors.New("extension configuration exceeds frame limit")
	}
	raw, err := proto.Marshal(&v2.Request{Handshake: v2.Handshake, Operation: operation, Capability: cap, ConfigJson: config})
	if err != nil || len(raw) > v2.MaxFrame {
		return nil, errors.New("extension request exceeds frame limit")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out boundedExtensionOutput
	// No file system, environment, host sockets, credentials, or custom host imports.
	mod, err := e.vm.InstantiateModule(ctx, e.module, wazero.NewModuleConfig().WithName("").WithStdin(bytes.NewReader(raw)).WithStdout(&out))
	if mod != nil {
		defer mod.Close(ctx)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if out.exceeded {
		return nil, errors.New("extension output exceeds frame limit")
	}
	if err != nil {
		slog.Warn("plugin_invocation_failed", "plugin_id", e.manifest.ID, "operation", operation, "reason", "guest trap or invalid import")
		return nil, errors.New("extension trapped or failed to start")
	}
	var reply v2.Reply
	if proto.Unmarshal(out.Bytes(), &reply) != nil || len(reply.ProtoReflect().GetUnknown()) > 0 || reply.Handshake != v2.Handshake || reply.PluginId != e.manifest.ID || reply.Version != e.manifest.Version {
		return nil, extensionDenied(e.manifest.ID, cap, "invalid runtime identity or reply")
	}
	sort.Strings(reply.Capabilities)
	if !slices.Equal(reply.Capabilities, e.caps) {
		return nil, extensionDenied(e.manifest.ID, cap, "runtime capabilities differ from signed declaration")
	}
	if !reply.Accepted {
		return nil, extensionDenied(e.manifest.ID, cap, "runtime rejected operation")
	}
	return &reply, nil
}

// Only configuration and health methods reach this adapter. Provider forwarding
// resumes on the host's existing authorized HTTP path after an allow decision.
type extensionConfigClient struct {
	ext *extensionRuntime
}

func (c *extensionConfigClient) GetInfo(context.Context, *v1.GetInfoRequest, ...grpc.CallOption) (*v1.GetInfoResponse, error) {
	return &v1.GetInfoResponse{PluginId: c.ext.manifest.ID, PluginVersion: c.ext.manifest.Version, Capabilities: slices.Clone(c.ext.caps), ProtocolVersion: v2.ProtocolVersion, TransportApiVersion: 1}, nil
}
func (c *extensionConfigClient) Forward(context.Context, ...grpc.CallOption) (grpc.BidiStreamingClient[v1.ForwardRequest, v1.ForwardResponse], error) {
	return nil, extensionDenied(c.ext.manifest.ID, "egress", "v2 outbound transport runs on the host")
}
func (c *extensionConfigClient) Health(ctx context.Context, _ *v1.HealthRequest, _ ...grpc.CallOption) (*v1.HealthResponse, error) {
	err := c.ext.trusted()
	return &v1.HealthResponse{Healthy: err == nil && !c.ext.closed.Load()}, err
}
func (c *extensionConfigClient) ValidateConfig(ctx context.Context, req *v1.ValidateConfigRequest, _ ...grpc.CallOption) (*v1.ValidateConfigResponse, error) {
	res, err := c.ext.exchange(ctx, "configure", "", req.ConfigJson)
	if err != nil {
		return nil, err
	}
	return &v1.ValidateConfigResponse{Valid: true, NormalizedConfigJson: res.NormalizedConfigJson}, nil
}
func (c *extensionConfigClient) ApplyConfig(_ context.Context, req *v1.ApplyConfigRequest, _ ...grpc.CallOption) (*v1.ApplyConfigResponse, error) {
	cfg := slices.Clone(req.ConfigJson)
	c.ext.config.Store(&cfg)
	return &v1.ApplyConfigResponse{Applied: true}, nil
}
func (c *extensionConfigClient) TestConfig(ctx context.Context, req *v1.TestConfigRequest, _ ...grpc.CallOption) (*v1.TestConfigResponse, error) {
	_, err := c.ext.exchange(ctx, "test", "", req.ConfigJson)
	return &v1.TestConfigResponse{Success: err == nil}, err
}

func (r *pluginRuntime) exited() bool {
	if r.extension != nil {
		return r.extension.closed.Load()
	}
	return r.client == nil || r.client.Exited()
}

func (r *pluginRuntime) authorize(ctx context.Context, cap string) error {
	if !r.beginRequest() {
		return extensionDenied(r.installation.PluginKey, cap, "extension draining")
	}
	defer r.finishRequest()
	if err := extensionGrant(r.installation, cap); err != nil {
		return err
	}
	var cfg []byte
	if p := r.extension.config.Load(); p != nil {
		cfg = *p
	}
	_, err := r.extension.exchange(ctx, "authorize", cap, cfg)
	return err
}
func extensionGrant(p *PluginInstallation, cap string) error {
	for _, decl := range p.Manifest.Capabilities {
		if decl.ID != cap {
			continue
		}
		for _, b := range p.Bindings {
			if b.Enabled && b.Capability == decl.ID && b.AccountType == decl.AccountType && b.Platform == decl.Platform {
				return nil
			}
		}
	}
	return extensionDenied(p.PluginKey, cap, "signed declaration and enabled binding required")
}
