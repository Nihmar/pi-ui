package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Per-session runtime plumbing and the bridge config document of §5.7 / PLAN.md §3.3.
const (
	// bridgeConfigEnv is the variable the bridge extension reads its config from.
	bridgeConfigEnv = "PI_UI_BRIDGE_CONFIG"
	// runtimeDirEnv is the single source of the runtime directory: the CLI resolves the
	// same variable, so both agree without Config carrying a path.
	runtimeDirEnv = "PIUI_RUNTIME_DIR"
	// runtimeDirName is the directory name under $XDG_RUNTIME_DIR (and the prefix under
	// os.TempDir() when there is no session bus).
	runtimeDirName = "pi-ui"

	// approveModeConfirm lets the bridge ask the operator about a matching command;
	// the spike always asks, so the approval dialog is exercised end to end.
	approveModeConfirm = "confirm"

	flagSessionDir = "--session-dir"
	flagName       = "--name"
	flagExtension  = "-e"
)

// approvalPatterns are the bash commands the bridge stops for (§5.7). They are a
// deliberately small, obviously dangerous set: the spike demonstrates the dialog path, it
// does not claim to be a policy engine.
var approvalPatterns = []string{"rm -rf", "git push --force", "sudo"}

// bridgeConfig is the per-session document written for the bridge extension.
type bridgeConfig struct {
	SessionID string          `json:"sessionId"`
	Approvals bridgeApprovals `json:"approvals"`
	// MCPConfig is the file the bridge reads for the MCP servers it should connect
	// to. It is a path, not the configuration: one document serves every session, and
	// a change takes effect at the next spawn.
	MCPConfig string `json:"mcpConfig,omitempty"`
}

// bridgeApprovals is the approval policy of one session.
type bridgeApprovals struct {
	Mode     string   `json:"mode"` // "confirm" | "off"
	Patterns []string `json:"patterns"`
}

// RuntimeDir is the directory that holds the per-session runtime files, today the
// PI_UI_BRIDGE_CONFIG documents: PIUI_RUNTIME_DIR when set, else $XDG_RUNTIME_DIR/pi-ui,
// else os.TempDir()/pi-ui-<uid> (docs/spike-interfaces.md §5.7).
//
// Config deliberately carries no runtime-dir field: PIUI_RUNTIME_DIR is the one source of
// truth, so a caller that resolves the environment once (cli/serve) and a caller that
// does not (a test) still agree on the path. Files are never deleted here: the default
// directory is cleaned up by the OS or by whoever owns PIUI_RUNTIME_DIR.
func RuntimeDir() string {
	if dir := os.Getenv(runtimeDirEnv); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, runtimeDirName)
	}
	return filepath.Join(os.TempDir(), runtimeDirName+"-"+strconv.Itoa(os.Getuid()))
}

// childArgv builds the argv of one child: the Spec's command (or Config.PiCommand), plus
// the per-session flags the Spec asks for. A flag the caller already passed is never
// duplicated, so Spec.Command stays authoritative for provider/model/thinking flags.
func (m *Manager) childArgv(spec Spec) []string {
	argv := spec.Command
	if len(argv) == 0 {
		argv = m.cfg.PiCommand
	}
	argv = append([]string(nil), argv...)

	if dir := firstNonEmpty(spec.SessionDir, m.cfg.SessionDir); dir != "" && !hasFlag(argv, flagSessionDir) {
		argv = append(argv, flagSessionDir, dir)
	}
	if spec.Name != "" && !hasFlag(argv, flagName) {
		argv = append(argv, flagName, spec.Name)
	}
	if ext := firstNonEmpty(spec.BridgeExt, m.cfg.BridgeExt); ext != "" && !hasFlag(argv, flagExtension) {
		argv = append(argv, flagExtension, ext)
	}
	return argv
}

// childEnv returns the extra environment of one child: the Spec's entries in a stable
// order, plus PI_UI_BRIDGE_CONFIG when the child loads the bridge extension.
func (m *Manager) childEnv(spec Spec, argv []string, sessionID string) ([]string, error) {
	env := make([]string, 0, len(spec.Env)+1)
	for _, key := range sortedKeys(spec.Env) {
		env = append(env, key+"="+spec.Env[key])
	}
	if !hasFlag(argv, flagExtension) {
		return env, nil
	}
	path, err := writeBridgeConfig(sessionID, m.cfg.MCPConfig)
	if err != nil {
		return nil, err
	}
	return append(env, bridgeConfigEnv+"="+path), nil
}

// writeBridgeConfig writes <runtime>/<sid>.json and returns its path. The document is
// written through a temporary file and renamed, so a child that starts reading
// immediately never sees a half-written JSON object; it is 0600 because it is
// per-session state.
func writeBridgeConfig(sessionID, mcpConfig string) (string, error) {
	dir := RuntimeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("sessions: runtime dir %s: %w", dir, err)
	}
	document := bridgeConfig{
		SessionID: sessionID,
		Approvals: bridgeApprovals{Mode: approveModeConfirm, Patterns: approvalPatterns},
		MCPConfig: mcpConfig,
	}
	data, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("sessions: bridge config: %w", err)
	}

	tmp, err := os.CreateTemp(dir, sessionID+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("sessions: bridge config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", fmt.Errorf("sessions: bridge config %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("sessions: bridge config %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("sessions: bridge config %s: %w", tmpName, err)
	}

	path := filepath.Join(dir, sessionID+".json")
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("sessions: bridge config %s: %w", path, err)
	}
	return path, nil
}

// hasFlag reports whether argv already carries name, in "--name value" or "--name=value"
// form; both spellings must suppress the flag the supervisor would add.
func hasFlag(argv []string, name string) bool {
	for _, arg := range argv {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

// sortedKeys returns the keys of a map in lexicographic order, for a deterministic child
// environment (a test that compares argv/env must not depend on map iteration order).
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
