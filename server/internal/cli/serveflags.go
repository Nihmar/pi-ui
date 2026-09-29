package cli

import (
	"errors"
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
	envIdleTimeout   = "PIUI_IDLE_TIMEOUT"
	envWrapUpBudget  = "PIUI_WRAP_UP_BUDGET"
	envWrapUpPrompt  = "PIUI_WRAP_UP_PROMPT"
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

	// Idle handling of PLAN.md §4.2: an hour of silence is a wrap-up, not a kill.
	defaultIdleTimeout  = time.Hour
	defaultWrapUpBudget = time.Minute
	// defaultWrapUpPrompt asks for the handoff note the plan requires before an idle
	// session is closed. It is a prompt, so it goes through the ordinary conversation.
	defaultWrapUpPrompt = "You have been idle for a while and the server is about to close this session. " +
		"Write a short handoff note (where we are, next steps, open questions) so the work can be resumed later, then finish."
)

// serveConfig is the resolved configuration of one `pi-ui serve`.
type serveConfig struct {
	addr           string
	pi             string
	bridge         string
	token          string
	logLevel       string
	maxSessions    int
	replayEvents   int
	replayWindow   time.Duration
	dialogTimeout  time.Duration
	heartbeat      time.Duration
	allowHosts     []string
	allowOrigins   []string
	stateDir       string
	rateRest       int
	rateRefresh    int
	rateWS         int
	ratePrompt     int
	idleTimeout    time.Duration
	wrapUpBudget   time.Duration
	wrapUpPrompt   string
	sessionFlags   []string
	roots          []string
	sessionDirs    []string
	mcpConfig      string
	tlsCert        string
	tlsKey         string
	allowIPs       []string
	isolateImage   string
	isolateDocker  string
	isolateUser    string
	isolateNetwork string
	isolateMounts  []string
	updateCommand  string
	piVersion      string
	terminals      int
}

// rootFlag collects the repeatable --root flag: the workspaces a client may browse.
type rootFlag []string

// String implements flag.Value.
func (r *rootFlag) String() string { return strings.Join(*r, ",") }

// Set implements flag.Value.
func (r *rootFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("an empty workspace is not a workspace")
	}
	*r = append(*r, value)
	return nil
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
	fs.String("terminals", "", "PTY terminals a client may hold open at once (default 4)")
	fs.String("session-dirs", "", "comma-separated directories holding pi session JSONL, for the message search")
	fs.String("mcp-config", "", "MCP server configuration file (default <state-dir>/mcp.json when a state directory exists)")
	fs.String("update-command", "", "script the server runs to apply updates (empty = managed elsewhere)")
	fs.String("tls-cert", "", "certificate file; with --tls-key the server terminates TLS (PIUI_TLS_CERT)")
	fs.String("tls-key", "", "private key file for --tls-cert (PIUI_TLS_KEY)")
	fs.String("allow-ips", "", "comma-separated peer addresses or CIDR blocks allowed to connect (empty = any)")
	fs.String("isolate", "", "run every session in a container of this image (empty = on the host)")
	fs.String("isolate-docker", "", "container CLI to use with --isolate (default docker)")
	fs.String("isolate-user", "", "uid:gid the containers run as (default this process's)")
	fs.String("isolate-network", "", "network mode for isolated sessions: host, bridge or none (default host)")
	fs.String("pi-version", "", "version of the pi binary this server runs, for the update panel (PIUI_PI_VERSION)")
	fs.String("idle-timeout", "", "wrap up a session after this much silence, 0 = off (default 1h)")
	fs.String("wrap-up-budget", "", "time the handoff turn gets before the session stops (default 1m)")
	fs.String("wrap-up-prompt", "", "what an idle session is asked before it stops (PIUI_WRAP_UP_PROMPT)")
	var sessionArgs sessionFlag
	fs.Var(&sessionArgs, "session", "session to start at boot: <cwd>[:<name>] (repeatable)")
	var rootArgs rootFlag
	fs.Var(&rootArgs, "root", "workspace the client may browse: <path> (repeatable)")
	var isolateMountArgs rootFlag
	fs.Var(&isolateMountArgs, "isolate-mount", "extra host[:container] path to mount, repeatable")

	if err := fs.Parse(args); err != nil {
		return serveConfig{}, Usage(err)
	}
	if fs.NArg() > 0 {
		return serveConfig{}, Usagef("unexpected argument %q", fs.Arg(0))
	}

	cfg := serveConfig{
		addr:           resolve(fs, "addr", envAddr, defaultAddr),
		pi:             resolve(fs, "pi", envPi, defaultPi),
		bridge:         resolve(fs, "bridge", envBridge, ""),
		token:          resolve(fs, "token", envToken, ""),
		logLevel:       resolve(fs, "log-level", envLogLevel, defaultLogLevel),
		allowHosts:     splitList(resolve(fs, "allow-hosts", envAllowHosts, "")),
		allowOrigins:   splitList(resolve(fs, "allow-origins", envAllowOrigins, "")),
		stateDir:       resolve(fs, "state-dir", envStateDir, ""),
		sessionFlags:   sessionArgs,
		roots:          rootArgs,
		sessionDirs:    splitList(resolve(fs, "session-dirs", "", "")),
		mcpConfig:      resolve(fs, "mcp-config", "PIUI_MCP_CONFIG", ""),
		updateCommand:  resolve(fs, "update-command", "PIUI_UPDATE_COMMAND", ""),
		tlsCert:        resolve(fs, "tls-cert", "PIUI_TLS_CERT", ""),
		tlsKey:         resolve(fs, "tls-key", "PIUI_TLS_KEY", ""),
		allowIPs:       splitList(resolve(fs, "allow-ips", "PIUI_ALLOW_IPS", "")),
		isolateImage:   resolve(fs, "isolate", "PIUI_ISOLATE", ""),
		isolateDocker:  resolve(fs, "isolate-docker", "PIUI_ISOLATE_DOCKER", ""),
		isolateUser:    resolve(fs, "isolate-user", "PIUI_ISOLATE_USER", ""),
		isolateNetwork: resolve(fs, "isolate-network", "PIUI_ISOLATE_NETWORK", ""),
		isolateMounts:  isolateMountArgs,
		piVersion:      resolve(fs, "pi-version", "PIUI_PI_VERSION", ""),
	}

	var err error
	if cfg.maxSessions, err = resolveInt(fs, "max-sessions", envMaxSessions, sessions.DefaultMaxSessions); err != nil {
		return serveConfig{}, err
	}
	if cfg.replayEvents, err = resolveInt(fs, "replay-events", envReplayEvents, defaultReplayEvents); err != nil {
		return serveConfig{}, err
	}
	if cfg.terminals, err = resolveInt(fs, "terminals", "PIUI_TERMINALS", 4); err != nil {
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
	if cfg.idleTimeout, err = resolveDurationOrZero(fs, "idle-timeout", envIdleTimeout, defaultIdleTimeout); err != nil {
		return serveConfig{}, err
	}
	if cfg.wrapUpBudget, err = resolveDuration(fs, "wrap-up-budget", envWrapUpBudget, defaultWrapUpBudget); err != nil {
		return serveConfig{}, err
	}
	cfg.wrapUpPrompt = resolve(fs, "wrap-up-prompt", envWrapUpPrompt, defaultWrapUpPrompt)
	return cfg, nil
}

// resolveDurationOrZero is resolveDuration with an explicit off switch: 0 is "disabled"
// and anything else must be a positive duration.
func resolveDurationOrZero(fs *flag.FlagSet, name, env string, fallback time.Duration) (time.Duration, error) {
	raw := resolve(fs, name, env, "")
	if strings.TrimSpace(raw) == "0" {
		return 0, nil
	}
	return resolveDuration(fs, name, env, fallback)
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
		"  --root <path>              workspace a client may browse, repeatable\n"+
		"  --terminals N              PTY terminals a client may hold open (default 4)\n"+
		"  --mcp-config PATH          MCP servers the bridge connects to (default <state-dir>/mcp.json)\n"+
		"  --update-command PATH      script that applies updates (empty = managed elsewhere)\n"+
		"  --tls-cert, --tls-key      terminate TLS with this certificate and key\n"+
		"  --allow-ips LIST           peer addresses or CIDR blocks allowed to connect\n"+
		"  --isolate IMAGE            run every session inside a container\n"+
		"  --isolate-network MODE     host, bridge or none for those containers (default host)\n"+
		"  --session-dirs LIST        directories of pi session JSONL for the message search\n"+
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
		"  --idle-timeout D           wrap up a session after D of silence, 0 = off (PIUI_IDLE_TIMEOUT)\n"+
		"  --wrap-up-budget D         time the handoff turn gets (PIUI_WRAP_UP_BUDGET)\n"+
		"  --wrap-up-prompt TEXT      what an idle session is asked (PIUI_WRAP_UP_PROMPT)\n"+
		"\n"+
		"environment only: PIUI_RUNTIME_DIR (per-session runtime files, default\n"+
		"$XDG_RUNTIME_DIR/pi-ui or os.TempDir()/pi-ui-<uid>)\n\n"+
		"Every flag can be supplied through its PIUI_* variable instead; a flag wins.\n"+
		"The process logs to stderr, prints the listening address on stdout and shuts down\n"+
		"gracefully on SIGINT/SIGTERM, reaping every child within two seconds.\n\n"+
		"Device access: without --token the server mints device tokens through pairing.\n"+
		"On first start it prints the pairing card — code, origin, link and QR — on stderr;\n"+
		"`pi-ui pair` prints one on demand. A revoked or lost device can always be recovered\n"+
		"with the admin password (`pi-ui auth set-password`), and --token selects the\n"+
		"static-token mode instead.\n")
}
