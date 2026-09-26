package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Nihmar/pi-ui/server/internal/auth"
	"github.com/Nihmar/pi-ui/server/internal/store"
)

// Environment variables and defaults of the server state.
const (
	// envStateDir overrides where the state database lives.
	envStateDir = "PIUI_STATE_DIR"
	// envAdminPassword supplies the admin password to `pi-ui auth set-password` for
	// scripted installs.
	envAdminPassword = "PIUI_ADMIN_PASSWORD"
	// stateDatabase is the file inside the state directory.
	stateDatabase = "state.db"
)

// resolveStateDir resolves where the state database lives: the flag wins, then
// PIUI_STATE_DIR, then $XDG_STATE_HOME/pi-ui, then ~/.local/state/pi-ui. Every path is
// returned absolute, so a server started from another directory finds the same file.
func resolveStateDir(flagValue string) (string, error) {
	if value := strings.TrimSpace(flagValue); value != "" {
		return filepath.Abs(value)
	}
	if value := strings.TrimSpace(os.Getenv(envStateDir)); value != "" {
		return filepath.Abs(value)
	}
	if value := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); value != "" {
		return filepath.Join(value, "pi-ui"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no state directory is known: %w (set %s)", err, envStateDir)
	}
	return filepath.Join(home, ".local", "state", "pi-ui"), nil
}

// authInviteKindQR is the invitation kind a server bootstraps with: code plus secret,
// so a photographed screen is not enough to type a code from.
const authInviteKindQR = auth.InviteQR

// openState creates the state directory when missing and opens the state database with
// the auth service over it. The caller closes the returned database.
func openState(ctx context.Context, dir string) (*store.DB, *auth.Service, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("state directory %s: %w", dir, err)
	}
	db, err := store.Open(ctx, filepath.Join(dir, stateDatabase))
	if err != nil {
		return nil, nil, err
	}
	// Production defaults: argon2id at 64 MiB, 30-day sliding tokens, 10-minute
	// invitations, invitations shared through the same file.
	service := auth.New(db.Auth(), auth.Options{Invites: db.Auth()})
	return db, service, nil
}
