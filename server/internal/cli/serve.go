package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/api"
	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/ratelimit"
	"github.com/Nihmar/pi-ui/server/internal/search"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/settings"
	"github.com/Nihmar/pi-ui/server/internal/terminal"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// Timeouts of the server loop.
const (
	// readHeaderTimeout bounds a client that connects and then stalls before sending a
	// request line. There is deliberately no WriteTimeout: it would kill the websocket.
	readHeaderTimeout = 10 * time.Second
	// httpShutdownTimeout bounds the drain of in-flight REST requests. Websockets are
	// closed by hub.Close() before this runs, so the drain is short by construction.
	httpShutdownTimeout = 3 * time.Second
)

// Serve runs the pi-ui server (docs/spike-interfaces.md §5.7): one HTTP listener serving
// /api/v1 and /ws/v1, plus one `pi --mode rpc` child per session, all reaped on ctx
// cancellation.
//
// It is the Run function of the `serve` command, so args are the arguments that follow the
// command name. stdout receives the single line a caller parses (the listening address) and
// stderr receives the logs, which never carry conversation content.
//
// A cancelled context (SIGINT/SIGTERM) is a clean stop, not a failure: Serve stops accepting
// requests, closes the hub, shades the sessions down and returns nil once every child is
// reaped, so a supervisor sees a successful exit.
func Serve(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cfg, err := parseServeConfig(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// --help is a request, not a failure: the usage text is already on stderr.
			return nil
		}
		return err
	}
	logger, err := newLogger(stderr, cfg.logLevel)
	if err != nil {
		return Usagef("%v", err)
	}
	specs, err := cfg.startSpecs()
	if err != nil {
		return err
	}

	// The state database is opened before anything serves: a server that cannot
	// remember its devices and password must not start.
	stateDir, err := resolveStateDir(cfg.stateDir)
	if err != nil {
		return err
	}
	stateDB, authService, err := openState(ctx, stateDir)
	if err != nil {
		return err
	}
	defer stateDB.Close()

	// The trail lives in the same state database, pruned by retention; both transports
	// write to it and only an admin can read it back.
	auditLog := audit.New(stateDB.Audit(), audit.Options{Logger: logger})

	// One authenticator for both transports: REST and the WebSocket handshake answer
	// the same question about the same request, whether that is a device token or the
	// static-token compatibility mode.
	var authenticator api.Authenticator
	if cfg.token != "" {
		authenticator = api.NewLoopbackOrToken(cfg.token)
	} else {
		authenticator = api.DeviceAuthenticator{Service: authService}
	}

	// One hub, one supervisor, one router: the supervisor is wired into the hub as command
	// handler, dialog handler and replay source, which is the whole cross-package seam.
	hub := ws.New(ws.Options{
		Authorizer:    restToWS{auth: authenticator},
		Audit:         auditLog,
		AllowHosts:    cfg.allowHosts,
		AllowOrigins:  cfg.allowOrigins,
		ReplayEvents:  cfg.replayEvents,
		ReplayWindow:  cfg.replayWindow,
		Heartbeat:     cfg.heartbeat,
		ServerVersion: Version,
		PiVersion:     "",
		Features:      cfg.features(),
		Limits:        cfg.limits(),
	})
	if cfg.token == "" {
		// Revoking a device closes its sockets at once, from wherever the revoke came.
		authService.SetRevokeHook(func(deviceID string) {
			if closer, ok := hub.(ws.DeviceCloser); ok {
				closer.CloseDevice(deviceID)
			}
		})
	}
	supervisor := sessions.New(sessions.Config{
		PiCommand:     []string{cfg.pi, "--mode", "rpc"},
		BridgeExt:     cfg.bridge,
		MaxSessions:   cfg.maxSessions,
		DialogTimeout: cfg.dialogTimeout,
		PromptLimit:   cfg.ratePrompt,
		IdleTimeout:   cfg.idleTimeout,
		WrapUpBudget:  cfg.wrapUpBudget,
		WrapUpPrompt:  cfg.wrapUpPrompt,
		Logger:        logger,
		Hub:           hub,
	})
	hub.SetCommandHandler(supervisor)
	hub.SetDialogHandler(supervisor)
	hub.SetReplayer(supervisor)

	var terminalsOf *terminal.Manager

	options := api.Options{
		Supervisor: supervisor,
		Hub:        hub,
		Info: api.ServerInfo{
			Version: Version,
			// The spike does not probe pi: Phase 3 adds the version probe together with
			// the "degrade instead of fail" policy of AGENTS.md.
			PiVersion: "",
			Features:  cfg.features(),
			Limits:    cfg.limits(),
		},
	}
	if len(cfg.roots) > 0 || len(cfg.sessionDirs) > 0 {
		// The workspaces are the only host directories a client may reach, and the
		// session directories are the one extra place the message search reads. A
		// server started with neither exposes no filesystem at all (a 501, never a
		// guess); one started with only session dirs gets them as its roots, so even
		// the file half of a search stays inside them.
		files, err := fs.New(fs.Config{Roots: searchRoots(cfg.roots, cfg.sessionDirs)})
		if err != nil {
			return err
		}
		options.FS = files
		found, err := search.New(search.Config{FS: files, SessionDirs: cfg.sessionDirs})
		if err != nil {
			return err
		}
		options.Search = found
		if len(cfg.roots) > 0 {
			// A PTY is the one capability that runs arbitrary commands, so it exists
			// only where a real workspace was configured: session directories are for
			// reading, not for running a shell in.
			terminals, err := newTerminals(files, hub, cfg.terminals)
			if err != nil {
				return err
			}
			terminalsOf = terminals
		}
	}

	if stateDB != nil {
		// The settings live in the state database, and a running server publishes a
		// change so a client showing them notices another device's edit.
		options.Settings = settings.New(stateDB.Settings(), hub)
	}

	options.Auth = authenticator
	options.Audit = auditLog
	options.RateLimit = ratelimit.New(cfg.rateRest)
	options.RefreshRateLimit = ratelimit.New(cfg.rateRefresh)
	// The connect budget wraps the hub: the handshake still authenticates, but only
	// after a token has been spent for this client.
	options.Hub = connectLimitedHub{Hub: hub, limit: ratelimit.New(cfg.rateWS), audit: auditLog}
	if cfg.token == "" {
		// Pairing endpoints only exist in device mode; --token keeps the static-token
		// compatibility mode of the spike.
		options.AuthService = authService
	}
	router := api.NewRouter(options)

	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.addr, err)
	}
	server := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	logger.Info("pi-ui: serving",
		"addr", listener.Addr().String(),
		"version", Version,
		"commit", Commit,
		"pi", cfg.pi,
		"maxSessions", cfg.maxSessions,
		"runtimeDir", sessions.RuntimeDir(),
		"stateDir", stateDir,
		"bridge", cfg.bridge != "",
		"tokenRequired", cfg.token != "",
		"deviceAuth", cfg.token == "",
		"rateRest", cfg.rateRest,
		"rateRefresh", cfg.rateRefresh,
		"rateWS", cfg.rateWS,
		"ratePrompt", cfg.ratePrompt,
		"idleTimeout", cfg.idleTimeout.String(),
	)
	// The one line on stdout: a supervisor or a test needs the address it actually bound
	// (with --addr 127.0.0.1:0 the port is the kernel's choice).
	fmt.Fprintf(stdout, "listening %s\n", listener.Addr().String())

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	// Bootstrap: a server with neither a device nor a password has no way in yet, so it
	// mints one invitation and logs it. The QR comes from `pi-ui pair`, which writes to
	// the same state database.
	if cfg.token == "" && !authService.HasAdminPassword() && len(authService.Devices()) == 0 && authService.PendingInvites() == 0 {
		if invite, inviteErr := authService.NewInvite(authInviteKindQR); inviteErr != nil {
			logger.Error("pi-ui: could not mint the bootstrap pairing invitation", "error", inviteErr)
		} else {
			logger.Info("pi-ui: no device is paired yet; minted a pairing invitation",
				"code", invite.Code,
				"expiresInSec", int(time.Until(invite.ExpiresAt).Seconds()),
				"hint", "run `pi-ui pair --url http://<host>:<port>` on this host for a scannable QR")
		}
	}

	// Boot sessions after the listener is up, so a client can watch them spawn. A session
	// that does not start is logged and skipped: the server keeps serving.
	for _, spec := range specs {
		info, startErr := supervisor.Start(ctx, spec)
		if startErr != nil {
			logger.Error("pi-ui: session did not start", "cwd", spec.CWD, "error", startErr)
			continue
		}
		logger.Info("pi-ui: session started", "sessionId", info.ID, "cwd", info.CWD, "pid", info.PID)
	}

	select {
	case <-ctx.Done():
		logger.Info("pi-ui: shutting down")
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			_ = supervisor.Shutdown(context.Background())
			_ = listener.Close()
			return fmt.Errorf("http server: %w", err)
		}
	}
	return shutdown(logger, server, hub, supervisor, terminalsOf)
}

// shutdown reaps the children first and drains HTTP afterwards: a SIGTERM must not wait for
// connected clients before the children start dying, which is what keeps acceptance
// criterion C8 (every child reaped within two seconds) true.
func shutdown(
	logger *slog.Logger,
	server *http.Server,
	hub ws.Hub,
	supervisor *sessions.Manager,
	terminals *terminal.Manager,
) error {
	reaped := make(chan error, 1)
	go func() {
		// The shells go first: a PTY outliving the process that showed it is a
		// command nobody can see any more.
		if terminals != nil {
			_ = terminals.Shutdown()
		}
		reaped <- supervisor.Shutdown(context.Background())
	}()

	// Closing the hub drops the websocket connections, so the drain below cannot wait on a
	// subscriber that simply stays connected.
	if err := hub.Close(); err != nil {
		logger.Warn("pi-ui: closing the hub", "error", err)
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(drainCtx); err != nil {
		logger.Warn("pi-ui: draining the http server", "error", err)
	}

	if err := <-reaped; err != nil {
		return err
	}
	logger.Info("pi-ui: stopped")
	return nil
}

// newLogger builds the process logger: slog on stderr, which keeps stdout free for the one
// line a caller parses and never mixes logs into a child's protocol stream.
func newLogger(stderr io.Writer, level string) (*slog.Logger, error) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("--log-level %q: want debug, info, warn or error", level)
	}
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: parsed})), nil
}

// features are the capability flags a client negotiates against (§5.4): a new capability adds
// a name here instead of changing a payload shape.
func (c serveConfig) features() []string {
	features := []string{"sessions", "replay", "extension_ui"}
	if c.bridge != "" {
		// The approval dialog path exists only when a bridge extension is configured.
		features = append(features, "approvals")
	}
	return features
}

// limits are the scalar bounds GET /api/v1/server reports (schemas/core.json limits).
func (c serveConfig) limits() map[string]any {
	return map[string]any{
		"maxSessions":      c.maxSessions,
		"replayEvents":     c.replayEvents,
		"replayWindowSec":  int(c.replayWindow.Seconds()),
		"dialogTimeoutSec": int(c.dialogTimeout.Seconds()),
		"heartbeatSec":     int(c.heartbeat.Seconds()),
	}
}

// searchRoots never invents a workspace a client could browse: a server configured
// only with session dirs still needs the filesystem service behind the search, so it
// is built over those directories and nothing else. /tmp as a root would be a hole,
// not a default.
func searchRoots(roots, sessionDirs []string) []string {
	if len(roots) > 0 {
		return roots
	}
	return sessionDirs
}
