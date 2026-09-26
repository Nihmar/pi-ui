package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// Environment variables serve resolves, in the order flag → variable → default
// (docs/spike-interfaces.md §5.7).
const (
	envAddr          = "PIUI_ADDR"
	envPi            = "PIUI_PI"
	envToken         = "PIUI_TOKEN"
	envLogLevel      = "PIUI_LOG_LEVEL"
	envMaxSessions   = "PIUI_MAX_SESSIONS"
	envReplayEvents  = "PIUI_REPLAY_EVENTS"
	envReplayWindow  = "PIUI_REPLAY_WINDOW"
	envDialogTimeout = "PIUI_DIALOG_TIMEOUT"
	envHeartbeat     = "PIUI_HEARTBEAT"
	envAllowHosts    = "PIUI_ALLOW_HOSTS"
	envAllowOrigins  = "PIUI_ALLOW_ORIGINS"
	envBridge        = "PIUI_BRIDGE"
	envRateRest      = "PIUI_RATE_REST"
	envRateRefresh   = "PIUI_RATE_REFRESH"
	envRateWS        = "PIUI_RATE_WS"
	envRatePrompt    = "PIUI_RATE_PROMPT"
)

// Defaults of the serve command.
const (
	defaultAddr         = "127.0.0.1:8787"
	defaultPi           = "pi"
	defaultLogLevel     = "info"
	defaultReplayEvents = 2000
	defaultReplayWindow = 15 * time.Minute
	defaultHeartbeat    = 30 * time.Second

	// Rate limits of PLAN.md §4.6: per token per minute. 0 disables one.
	defaultRateRest    = 120
	defaultRateRefresh = 10
	defaultRateWS      = 10
	defaultRatePrompt  = 30
)

// serveConfig is the resolved configuration of one `pi-ui serve`.
type serveConfig struct {
	addr          string
	pi            string
	bridge        string
	token         string
	logLevel      string
	maxSessions   int
	replayEvents  int
	replayWindow  time.Duration
	dialogTimeout time.Duration
	heartbeat     time.Duration
	allowHosts    []string
	allowOrigins  []string
	stateDir      string
	rateRest      int
	rateRefresh   int
	rateWS        int
	ratePrompt    int
	sessionFlags  []string
}

// sessionFlag collects the repeatable --session flag.
type sessionFlag []string

// String implements flag.Value.
func (s *sessionFlag) String() string { return strings.Join(*s, ",") }

// Set implements flag.Value.
func (s *sessionFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// resolve returns the flag's value when the caller set it, else the environment variable when
// it is non-empty, else the default (§5.7).
func resolve(fs *flag.FlagSet, name, env, fallback string) string {
	if isSet(fs, name) {
		if value := fs.Lookup(name).Value.String(); value != "" {
			return value
		}
	}
	if value := os.Getenv(env); value != "" {
		return value
	}
	return fallback
}

// resolveInt resolves an integer setting; a non-numeric value names the flag so the operator
// knows which knob to fix.
func resolveInt(fs *flag.FlagSet, name, env string, fallback int) (int, error) {
	raw := resolve(fs, name, env, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, Usagef("--%s: %q is not an integer", name, raw)
	}
	if value < 0 {
		return 0, Usagef("--%s: %d must not be negative", name, value)
	}
	return value, nil
}

// resolveDuration resolves a duration setting with Go's duration syntax ("15m", "60s").
func resolveDuration(fs *flag.FlagSet, name, env string, fallback time.Duration) (time.Duration, error) {
	raw := resolve(fs, name, env, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, Usagef("--%s: %q is not a duration (try 15m or 60s)", name, raw)
	}
	if value <= 0 {
		return 0, Usagef("--%s: %s must be positive", name, value)
	}
	return value, nil
}

// isSet reports whether the caller passed that flag explicitly.
func isSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// splitList splits a comma-separated setting and trims the entries, so "a, b" and "a,b" mean
// the same thing.
func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	list := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			list = append(list, trimmed)
		}
	}
	return list
}

// parseServeConfig resolves the command line: flags first, then the PIUI_* variables, then
// the defaults. A malformed number or duration is a usage error, never a silent default.
func parseServeConfig(args []string, stderr io.Writer) (serveConfig, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeServeUsage(stderr) }

	fs.String("addr", "", "listen address (default "+defaultAddr+")")
	fs.String("pi", "", "pi executable (default \""+defaultPi+"\")")
	fs.String("bridge", "", "path to the pi-ui-bridge extension every child loads with -e")
	fs.String("token", "", "bearer token clients must send (empty: loopback only)")
	fs.String("log-level", "", "log level: debug, info, warn or error (default info)")
	fs.String("max-sessions", "", "maximum live sessions (default 8)")
	fs.String("replay-events", "", "replay ring size in events (default 2000)")
	fs.String("replay-window", "", "replay ring age, e.g. 15m (default 15m)")
	fs.String("dialog-timeout", "", "time before an unanswered dialog is cancelled (default 60s)")
	fs.String("heartbeat", "", "server heartbeat interval (default 30s)")
	fs.String("allow-hosts", "", "comma-separated extra Host values accepted by /ws/v1")
	fs.String("allow-origins", "", "comma-separated extra Origin values accepted by /ws/v1")
	fs.String("state-dir", "", "state directory holding the SQLite database (PIUI_STATE_DIR)")
	fs.String("rate-rest", "", "REST requests per device per minute, 0 = off (default 120)")
	fs.String("rate-refresh", "", "token rotations per device per minute, 0 = off (default 10)")
	fs.String("rate-ws", "", "WebSocket connects per token per minute, 0 = off (default 10)")
	fs.String("rate-prompt", "", "prompts per session per minute, 0 = off (default 30)")
	var sessionArgs sessionFlag
	fs.Var(&sessionArgs, "session", "session to start at boot: <cwd>[:<name>] (repeatable)")

	if err := fs.Parse(args); err != nil {
		return serveConfig{}, Usage(err)
	}
	if fs.NArg() > 0 {
		return serveConfig{}, Usagef("unexpected argument %q", fs.Arg(0))
	}

	cfg := serveConfig{
		addr:         resolve(fs, "addr", envAddr, defaultAddr),
		pi:           resolve(fs, "pi", envPi, defaultPi),
		bridge:       resolve(fs, "bridge", envBridge, ""),
		token:        resolve(fs, "token", envToken, ""),
		logLevel:     resolve(fs, "log-level", envLogLevel, defaultLogLevel),
		allowHosts:   splitList(resolve(fs, "allow-hosts", envAllowHosts, "")),
		allowOrigins: splitList(resolve(fs, "allow-origins", envAllowOrigins, "")),
		stateDir:     resolve(fs, "state-dir", envStateDir, ""),
		sessionFlags: sessionArgs,
	}

	var err error
	if cfg.maxSessions, err = resolveInt(fs, "max-sessions", envMaxSessions, sessions.DefaultMaxSessions); err != nil {
		return serveConfig{}, err
	}
	if cfg.replayEvents, err = resolveInt(fs, "replay-events", envReplayEvents, defaultReplayEvents); err != nil {
		return serveConfig{}, err
	}
	if cfg.replayWindow, err = resolveDuration(fs, "replay-window", envReplayWindow, defaultReplayWindow); err != nil {
		return serveConfig{}, err
	}
	if cfg.dialogTimeout, err = resolveDuration(fs, "dialog-timeout", envDialogTimeout, sessions.DefaultDialogTimeout); err != nil {
		return serveConfig{}, err
	}
	if cfg.heartbeat, err = resolveDuration(fs, "heartbeat", envHeartbeat, defaultHeartbeat); err != nil {
		return serveConfig{}, err
	}
	if cfg.rateRest, err = resolveInt(fs, "rate-rest", envRateRest, defaultRateRest); err != nil {
		return serveConfig{}, err
	}
	if cfg.rateRefresh, err = resolveInt(fs, "rate-refresh", envRateRefresh, defaultRateRefresh); err != nil {
		return serveConfig{}, err
	}
	if cfg.rateWS, err = resolveInt(fs, "rate-ws", envRateWS, defaultRateWS); err != nil {
		return serveConfig{}, err
	}
	if cfg.ratePrompt, err = resolveInt(fs, "rate-prompt", envRatePrompt, defaultRatePrompt); err != nil {
		return serveConfig{}, err
	}
	return cfg, nil
}

// startSpecs turns the repeatable --session values into start specs.
func (c serveConfig) startSpecs() ([]sessions.Spec, error) {
	specs := make([]sessions.Spec, 0, len(c.sessionFlags))
	for _, value := range c.sessionFlags {
		cwd, name := splitSessionFlag(value)
		if cwd == "" {
			return nil, Usagef("--session %q: the working directory is empty", value)
		}
		specs = append(specs, sessions.Spec{CWD: cwd, Name: name})
	}
	return specs, nil
}

// splitSessionFlag splits "<cwd>[:<name>]". The name follows the last ':' and must not
// contain a path separator, so a path that happens to contain a colon stays a path.
func splitSessionFlag(value string) (cwd, name string) {
	index := strings.LastIndex(value, ":")
	if index <= 0 {
		return value, ""
	}
	candidate := value[index+1:]
	if candidate == "" || strings.ContainsAny(candidate, `/\`) {
		return value, ""
	}
	return value[:index], candidate
}

// writeServeUsage prints the command's own flags; cli.Run already documents the overall
// command line.
func writeServeUsage(w io.Writer) {
	fmt.Fprint(w, "usage: pi-ui serve [flags]\n\n"+
		"flags:\n"+
		"  --addr 127.0.0.1:8787      listen address (PIUI_ADDR)\n"+
		"  --pi \"pi\"                  pi executable (PIUI_PI)\n"+
		"  --session <cwd>[:<name>]   session to start at boot, repeatable\n"+
		"  --bridge <path>            pi-ui-bridge extension loaded by every child (PIUI_BRIDGE)\n"+
		"  --token <token>            bearer token clients must send (PIUI_TOKEN)\n"+
		"  --log-level <level>        debug, info, warn or error (PIUI_LOG_LEVEL)\n"+
		"  --max-sessions N           maximum live sessions (PIUI_MAX_SESSIONS)\n"+
		"  --replay-events N          replay ring size (PIUI_REPLAY_EVENTS)\n"+
		"  --replay-window D          replay ring age (PIUI_REPLAY_WINDOW)\n"+
		"  --dialog-timeout D         unanswered dialog deadline (PIUI_DIALOG_TIMEOUT)\n"+
		"  --heartbeat D              heartbeat interval (PIUI_HEARTBEAT)\n"+
		"  --allow-hosts a,b          extra Host values accepted by /ws/v1 (PIUI_ALLOW_HOSTS)\n"+
		"  --allow-origins a,b        extra Origin values accepted by /ws/v1 (PIUI_ALLOW_ORIGINS)\n"+
		"  --state-dir <path>         SQLite state database (PIUI_STATE_DIR)\n"+
		"  --rate-rest N              REST requests per device per minute (PIUI_RATE_REST)\n"+
		"  --rate-refresh N           token rotations per device per minute (PIUI_RATE_REFRESH)\n"+
		"  --rate-ws N                WebSocket connects per token per minute (PIUI_RATE_WS)\n"+
		"  --rate-prompt N            prompts per session per minute (PIUI_RATE_PROMPT)\n"+
		"\n"+
		"environment only: PIUI_RUNTIME_DIR (per-session runtime files, default\n"+
		"$XDG_RUNTIME_DIR/pi-ui or os.TempDir()/pi-ui-<uid>)\n\n"+
		"Every flag can be supplied through its PIUI_* variable instead; a flag wins.\n"+
		"The process logs to stderr, prints the listening address on stdout and shuts down\n"+
		"gracefully on SIGINT/SIGTERM, reaping every child within two seconds.\n\n"+
		"Device access: without --token the server mints device tokens through pairing.\n"+
		"On first start it logs a pairing code; `pi-ui pair --url <origin>` prints a QR.\n"+
		"A revoked or lost device can always be recovered with the admin password\n"+
		"(`pi-ui auth set-password`), and --token selects the static-token mode instead.\n")
}
