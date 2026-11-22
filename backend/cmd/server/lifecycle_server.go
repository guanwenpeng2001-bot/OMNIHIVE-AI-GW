package main

import (
	"context"
	"database/sql"
	"errors"
	"expvar"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/audit"
	process "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/runtime"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/routes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/web"
)

var auditWriteFailures = expvar.NewInt("audit_observation_write_failures")
var auditSweepFailures = expvar.NewInt("audit_retention_sweep_failures")

var servingObserver atomic.Pointer[audit.Observer]

func init() {
	expvar.Publish("audit_observations_dropped", expvar.Func(func() any {
		if observer := servingObserver.Load(); observer != nil {
			return observer.Dropped()
		}
		return uint64(0)
	}))
}

func checkDependencies(ctx context.Context, cfg *config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		return errors.New("invalid PostgreSQL connection configuration")
	}
	defer db.Close()
	if db.PingContext(ctx) != nil {
		return errors.New("required PostgreSQL dependency unavailable")
	}
	cache := repository.InitRedis(cfg)
	defer func() { _ = cache.Close() }()
	if cache.Ping(ctx).Err() != nil {
		return errors.New("required Redis dependency unavailable")
	}
	return nil
}

func configureLifecycle(app *Application) {
	s := app.Supervisor
	var degraded []string
	var recoveryFailed atomic.Bool
	if app.Restores.Journal == nil && s.Mode != process.Gateway {
		degraded = append(degraded, "restore")
	}
	if s.Mode.Serving() {
		for _, component := range []struct {
			name  string
			start func(context.Context) error
		}{{"plugins", app.PluginManager.Start}, {"prompt-audit", app.PromptAudit.Start}} {
			if err := component.start(context.Background()); err != nil {
				degraded = append(degraded, component.name)
				log.Printf("%s startup degraded: %v", component.name, err)
			}
		}
		servingObserver.Store(app.Audit.Observer)
		s.Add(func(ctx context.Context) {
			app.Audit.Observer.Run(ctx, app.Audit.Store, func() { auditWriteFailures.Add(1) })
		})
	}
	if s.Mode.Workers() {
		s.Add(app.Status.RunCollector)
		s.Add(func(ctx context.Context) { app.Audit.Store.RunRetention(ctx, func() { auditSweepFailures.Add(1) }) })
		if app.Restores.Journal != nil {
			s.Add(func(ctx context.Context) {
				stop := app.Restores.StartWorker(ctx)
				<-ctx.Done()
				stop()
			})
		}
		s.Add(func(ctx context.Context) {
			process.Every(ctx, 5*time.Minute, func(ctx context.Context) {
				err := app.ImageTaskCenter.Store.Recover(ctx)
				recoveryFailed.Store(err != nil && ctx.Err() == nil)
				if recoveryFailed.Load() {
					log.Print("image task recovery failed")
				}
			})
		})
	}
	if runner := app.ImageTaskCenter.Store.Runner; runner != nil {
		app.ImageTaskCenter.Store.Runner = &taskRunner{runner, app.Admission}
	}
	probe := routes.NewReadinessHandler(app.OpsMetrics.DependenciesReady, app.Admission.Ready)
	ready := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		names := degraded
		if recoveryFailed.Load() {
			names = slices.Concat(degraded, []string{"image-task-recovery"})
		}
		if len(names) != 0 {
			w.Header().Set("X-OmniHive-Degraded", strings.Join(names, ","))
		}
		probe.ServeHTTP(w, r)
	})
	app.Server.ConnContext = app.Admission.ConnContext
	app.Server.ConnState = app.Admission.ConnState
	app.Server.Handler = app.Admission.Wrap(app.Restores.Maintenance(roleHandler(s.Mode, ready, app.Server.Handler)))
}

func roleHandler(mode process.Mode, ready, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if r.URL.RawPath != "" {
			path = r.URL.RawPath
		} // matches the aggregate router's UseRawPath setting
		if path == "/ready" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-OmniHive-Mode", string(mode))
			ready.ServeHTTP(w, r)
			return
		}
		method := r.Method
		if method == http.MethodOptions {
			method = r.Header.Get("Access-Control-Request-Method")
		}
		owner := process.Role(method, path)
		if web.HasEmbeddedFrontend() && strings.TrimSuffix(path, "/") == "/usage" && (method == http.MethodGet || method == http.MethodHead) {
			owner = process.Control
		}
		if owner == "" || mode != process.All && owner != process.All && owner != mode {
			http.NotFound(w, r)
			return
		}
		if mode == process.Worker && (path != "/health" || (r.Method != http.MethodGet && r.Method != http.MethodHead)) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Task execution detaches from the submitting HTTP request. Its Prepared.Close
// runs after Finish persists the terminal/fenced result. Keep that lifetime in
// the same drain group as active handlers.
type taskRunner struct {
	inner     tasks.Runner
	admission *process.Admission
}

func (t *taskRunner) Prepare(ctx context.Context, scope tasks.Scope, work tasks.Work) (tasks.Prepared, error) {
	lifetime, release, ok := t.admission.Acquire()
	if !ok {
		return tasks.Prepared{}, tasks.Unavailable
	}
	plan, err := t.inner.Prepare(ctx, scope, work)
	if err != nil {
		release()
		return plan, err
	}
	run, closePlan := plan.Run, plan.Close
	plan.Run = func(ctx context.Context) tasks.Outcome {
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(lifetime, cancel)
		defer stop()
		defer cancel()
		if lifetime.Err() != nil {
			cancel()
		}
		return run(ctx)
	}
	plan.Close = func() { defer release(); closePlan() }
	return plan, nil
}

func runProfiles(ctx context.Context) {
	server := &http.Server{Addr: "127.0.0.1:6060", ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("pprof listener: %v", err)
		}
	}()
	<-ctx.Done()
	_ = server.Close()
	<-done
}

func shutdownApplication(ctx context.Context, app *Application) error {
	// Reserve the final third for worker joins, receipt persistence and pool close.
	drain, cancel := context.WithCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		cancel()
		drain, cancel = context.WithTimeout(ctx, time.Until(deadline)*2/3)
	}
	defer cancel()
	if app.Admission != nil {
		app.Admission.BeginDrain()
	}
	var imageDrain <-chan struct{}
	if app.AsyncImages != nil {
		imageDrain = app.AsyncImages.BeginDrain()
	}
	app.UpstreamJobs.BeginDrain()
	httpDone := make(chan error, 1)
	go func() { httpDone <- app.Server.Shutdown(drain) }()
	joined := make(chan struct{})
	go func() {
		if app.Admission != nil {
			app.Admission.Wait()
		}
		_ = app.UpstreamJobs.WaitDrain(context.Background())
		if imageDrain != nil {
			<-imageDrain
		}
		close(joined)
	}()
	var drainErr error
	select {
	case <-joined:
	case <-drain.Done():
		drainErr = drain.Err()
	}
	select {
	case err := <-httpDone:
		drainErr = errors.Join(drainErr, err)
	case <-drain.Done():
		drainErr = drain.Err()
	}
	if drainErr != nil {
		if app.AsyncImages != nil {
			expire, cancel := context.WithTimeout(ctx, 2*time.Second)
			app.AsyncImages.ExpireDrain(expire)
			cancel()
		}
		log.Print("HTTP drain deadline reached; closing active transports")
		_ = app.Server.Close()
		if app.Admission != nil {
			app.Admission.CloseHijacked()
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if app.Supervisor != nil {
			<-app.Supervisor.Stop()
		}
		<-joined
		app.Cleanup()
	}()
	select {
	case <-done:
		return drainErr
	case <-ctx.Done():
		return fmt.Errorf("shutdown deadline reached while joining workers and closing dependencies: %w", ctx.Err())
	}
}
