package main

//go:generate go run github.com/google/wire/cmd/wire

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	// #nosec G108 -- pprof exposure is opt-in via OMNIHIVE_PPROF and bound to 127.0.0.1:6060 only.
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/runtime"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	process "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/runtime"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/runtimeenv"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/setup"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/web"
	"github.com/spf13/viper"

	"github.com/gin-gonic/gin"
)

//go:embed VERSION
var embeddedVersion string

// Build-time variables (can be set by ldflags)
var (
	Version   = ""
	Commit    = "unknown"
	Date      = "unknown"
	BuildType = "source" // "source" for manual builds, "release" for CI builds (set by ldflags)
)

func init() {
	// 如果 Version 已通过 ldflags 注入（例如 -X main.Version=...），则不要覆盖。
	if strings.TrimSpace(Version) != "" {
		return
	}

	// 默认从 embedded VERSION 文件读取版本号（编译期打包进二进制）。
	Version = strings.TrimSpace(embeddedVersion)
	if Version == "" {
		Version = "0.0.0-dev"
	}
}

// initLogger configures the default slog handler based on gin.Mode().
// In non-release mode, Debug level logs are enabled.
func main() {
	logger.InitBootstrap()
	defer logger.Sync()

	// Parse command line flags
	setupMode := flag.Bool("setup", false, "Run setup wizard in CLI mode")
	showVersion := flag.Bool("version", false, "Show version information")
	roleFlag := flag.String("mode", "", "Process role (all, control, gateway, worker)")
	flag.Parse()

	if *showVersion {
		log.Printf("OmniHive %s (commit: %s, built: %s)\n", Version, Commit, Date)
		return
	}

	// CLI setup mode
	if *setupMode {
		if err := setup.RunCLI(); err != nil {
			log.Fatalf("Setup failed: %v", err)
		}
		return
	}

	role := strings.TrimSpace(*roleFlag)
	if role == "" {
		role = os.Getenv("OMNIHIVE_RUNTIME_MODE")
	}
	initialMode, err := process.ParseMode(role)
	if err != nil {
		log.Fatal(err)
	}
	// Check if setup is needed
	if setup.NeedsSetup() {
		if initialMode != process.All && initialMode != process.Control {
			log.Fatalf("%s mode requires an initialized installation", initialMode)
		}
		// Check if auto-setup is enabled (for Docker deployment)
		if setup.AutoSetupEnabled() {
			log.Println("Auto setup mode enabled...")
			if err := setup.AutoSetupFromEnv(); err != nil {
				log.Fatalf("Auto setup failed: %v", err)
			}
			// Continue to main server after auto-setup
		} else {
			log.Println("First run detected, starting setup wizard...")
			runSetupServer()
			return
		}
	}

	// Normal server mode
	if err := runMainServer(role); err != nil {
		log.Fatal(err)
	}
}

func runSetupServer() {
	r := gin.New()
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS(config.CORSConfig{}))
	r.Use(middleware.SecurityHeaders(config.CSPConfig{Enabled: true, Policy: config.DefaultCSPPolicy}, nil))

	// Register setup routes
	setup.RegisterRoutes(r)

	// Serve embedded frontend if available. The setup wizard is unusable without
	// a panel, so an unusable asset tree must stop startup with a clear record
	// instead of panicking or running a wizard nobody can open.
	if web.HasEmbeddedFrontend() {
		serveFrontend, err := web.ServeEmbeddedFrontend()
		if err != nil {
			log.Fatalf("embedded frontend assets are unusable: %v", err)
		}
		r.Use(serveFrontend)
	}

	// Get server address from config.yaml or environment variables (SERVER_HOST, SERVER_PORT)
	// This allows users to run setup on a different address if needed
	addr := config.GetServerAddress()
	log.Printf("Setup wizard available at http://%s", addr)
	log.Println("Complete the setup wizard to configure OmniHive")

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
		Protocols:         protocols,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Failed to start setup server: %v", err)
	}
}

func runMainServer(role string) error {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if role == "" {
		role = viper.GetString("runtime.mode")
	}
	mode, err := process.ParseMode(role)
	if err != nil {
		return err
	}
	if err := logger.Init(logger.OptionsFromConfig(cfg.Log)); err != nil {
		return fmt.Errorf("initialize logger: %w", err)
	}
	if cfg.RunMode == config.RunModeSimple {
		log.Print("SIMPLE mode disables billing and quota checks")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", cfg.Server.Address())
	if err != nil {
		return fmt.Errorf("bind %s listener: %w", mode, err)
	}
	defer func() { _ = listener.Close() }()
	if err := checkDependencies(ctx, cfg); err != nil {
		return err
	}
	app, err := initializeApplication(handler.BuildInfo{Version: Version, BuildType: BuildType}, cfg, process.New(mode))
	if err != nil {
		return fmt.Errorf("initialize %s application: %w", mode, err)
	}
	configureLifecycle(app)
	if runtimeenv.Get("OMNIHIVE_PPROF", "SUB2API_PPROF") == "1" {
		app.Supervisor.Add(runProfiles)
	}
	app.Supervisor.Start()
	served := make(chan error, 1)
	go func() { served <- app.Server.Serve(listener) }()
	log.Printf("OmniHive %s listening on %s", mode, listener.Addr())
	select {
	case <-ctx.Done():
	case err = <-served:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	budget, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return errors.Join(err, shutdownApplication(budget, app))
}
