// Package fs is the filesystem service of the server: the one place that decides
// which paths a client may touch.
//
// Everything it exposes is confined to the roots the operator configured on the
// command line, and confinement is enforced on the resolved path — symlinks
// included — before a single syscall touches the target. A path that leaves the
// roots is a `path_escape`, which is the audit action the plan names.
package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// Defaults bound what one request can move: a client must not be able to make the
// server read a gigabyte into memory to answer a REST call.
const (
	DefaultMaxReadBytes  = 1 << 20 // 1 MiB
	DefaultMaxWriteBytes = 1 << 20
)

// Root is one allowed workspace: the directory a client may browse, plus the id it
// is addressed by.
type Root struct {
	// ID is the short name a client sends as `rootId`; it is the last path segment.
	ID string `json:"id"`
	// Path is the absolute, symlink-resolved directory.
	Path string `json:"path"`
}

// Entry is one file or directory as the API reports it.
type Entry struct {
	// Name is the last path segment.
	Name string `json:"name"`
	// Path is the absolute path on the host.
	Path string `json:"path"`
	// Rel is the path relative to the root it lives under.
	Rel string `json:"rel"`
	// RootID is the root this entry belongs to.
	RootID string `json:"rootId"`
	// IsDir tells a listing what it is looking at.
	IsDir bool `json:"isDir"`
	// Size is the size in bytes (0 for a directory).
	Size int64 `json:"size"`
	// Mode is the permission bits as octal text ("0644").
	Mode string `json:"mode"`
	// ModTime is the last modification time, RFC3339 UTC.
	ModTime string `json:"modTime"`
	// Sha256 is the content hash of a file, so a writer can send back what it read
	// and be told when somebody else changed the file in between.
	Sha256 string `json:"sha256,omitempty"`
}

// Config tunes the service. Roots are the only required field.
type Config struct {
	// Roots are the allowed workspaces; the first one is the default.
	Roots []string
	// MaxReadBytes bounds one read (default DefaultMaxReadBytes).
	MaxReadBytes int64
	// MaxWriteBytes bounds one write (default DefaultMaxWriteBytes).
	MaxWriteBytes int64
}

// Service is the filesystem seam: one instance per server, injected everywhere a
// path has to be checked.
type Service struct {
	roots    []Root
	maxRead  int64
	maxWrite int64
}

// New builds the service, resolving every root (a root that does not exist is a
// configuration error, not a runtime surprise).
func New(cfg Config) (*Service, error) {
	if len(cfg.Roots) == 0 {
		return nil, errors.New("fs: at least one root is required")
	}
	roots := make([]Root, 0, len(cfg.Roots))
	seen := map[string]bool{}
	for _, raw := range cfg.Roots {
		if strings.TrimSpace(raw) == "" {
			return nil, errors.New("fs: an empty root is not a root")
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			return nil, fmt.Errorf("fs: root %q: %w", raw, err)
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("fs: root %q: %w", raw, err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("fs: root %q: %w", raw, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("fs: root %q is not a directory", raw)
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		roots = append(roots, Root{ID: filepath.Base(resolved), Path: resolved})
	}
	service := &Service{
		roots:    roots,
		maxRead:  cfg.MaxReadBytes,
		maxWrite: cfg.MaxWriteBytes,
	}
	if service.maxRead <= 0 {
		service.maxRead = DefaultMaxReadBytes
	}
	if service.maxWrite <= 0 {
		service.maxWrite = DefaultMaxWriteBytes
	}
	return service, nil
}

// Roots lists the allowed workspaces, so a client can offer them without asking.
func (s *Service) Roots() []Root { return append([]Root(nil), s.roots...) }

// List returns one directory's entries, directories first and then by name so two
// clients see the same order.
func (s *Service) List(path string) ([]Entry, error) {
	placement, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(placement.abs)
	if err != nil {
		return nil, notFound(path, err)
	}
	if !info.IsDir() {
		return nil, sessions.Codedf(sessions.CodeBadRequest, "%s is not a directory", path)
	}
	names, err := os.ReadDir(placement.abs)
	if err != nil {
		return nil, notFound(path, err)
	}
	entries := make([]Entry, 0, len(names))
	for _, name := range names {
		child, err := s.entry(placement, filepath.Join(placement.abs, name.Name()))
		if err != nil {
			// A file that disappeared between ReadDir and Stat is not a reason to
			// fail the whole listing.
			continue
		}
		entries = append(entries, child)
	}
	sortEntries(entries)
	return entries, nil
}

// Stat reports one path's metadata, content hash included for a file.
func (s *Service) Stat(path string) (Entry, error) {
	placement, err := s.resolve(path)
	if err != nil {
		return Entry{}, err
	}
	entry, err := s.entry(placement, placement.abs)
	if err != nil {
		return Entry{}, notFound(path, err)
	}
	return entry, nil
}

// Read returns a file's bytes, refusing anything above the cap (a client that wants
// more uses the download endpoint, which streams).
func (s *Service) Read(path string, maxBytes int64) ([]byte, Entry, error) {
	placement, err := s.resolve(path)
	if err != nil {
		return nil, Entry{}, err
	}
	entry, err := s.entry(placement, placement.abs)
	if err != nil {
		return nil, Entry{}, notFound(path, err)
	}
	if entry.IsDir {
		return nil, Entry{}, sessions.Codedf(sessions.CodeBadRequest, "%s is a directory", path)
	}
	limit := maxBytes
	if limit <= 0 || limit > s.maxRead {
		limit = s.maxRead
	}
	if entry.Size > limit {
		return nil, Entry{}, sessions.Codedf(sessions.CodeTooLarge,
			"%s is %d bytes; the read cap is %d", path, entry.Size, limit)
	}
	data, err := os.ReadFile(entry.Path)
	if err != nil {
		return nil, Entry{}, notFound(path, err)
	}
	return data, entry, nil
}

// Write stores bytes, refusing anything above the cap.
//
// A non-empty expectedSha256 makes the write conditional: it is the hash the
// client read, so a file somebody else changed in between is a conflict instead of
// a silent overwrite.
func (s *Service) Write(path string, data []byte, expectedSha256 string) (Entry, error) {
	if int64(len(data)) > s.maxWrite {
		return Entry{}, sessions.Codedf(sessions.CodeTooLarge,
			"%d bytes exceed the write cap of %d", len(data), s.maxWrite)
	}
	placement, err := s.resolve(path)
	if err != nil {
		return Entry{}, err
	}
	if expectedSha256 != "" {
		current, readErr := os.ReadFile(placement.abs)
		switch {
		case readErr == nil:
			sum := sha256.Sum256(current)
			if !strings.EqualFold(hex.EncodeToString(sum[:]), expectedSha256) {
				return Entry{}, sessions.Codedf(sessions.CodeBadRequest,
					"%s changed since it was read; reload before writing", path)
			}
		case errors.Is(readErr, os.ErrNotExist):
			// A conditional write cannot create: the client believed it existed.
			return Entry{}, sessions.Codedf(sessions.CodeBadRequest,
				"%s does not exist; drop the expected hash to create it", path)
		default:
			return Entry{}, notFound(path, readErr)
		}
	}
	if err := os.MkdirAll(filepath.Dir(placement.abs), 0o755); err != nil {
		return Entry{}, sessions.Codedf(sessions.CodeBadRequest,
			"the parent directory of %s cannot be used: %v", path, err)
	}
	if err := os.WriteFile(placement.abs, data, 0o644); err != nil {
		return Entry{}, sessions.Codedf(sessions.CodeBadRequest, "%s cannot be written: %v", path, err)
	}
	entry, err := s.entry(placement, placement.abs)
	if err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// Mkdir creates one directory (and its missing parents).
func (s *Service) Mkdir(path string) (Entry, error) {
	placement, err := s.resolve(path)
	if err != nil {
		return Entry{}, err
	}
	if err := os.MkdirAll(placement.abs, 0o755); err != nil {
		return Entry{}, sessions.Codedf(sessions.CodeBadRequest, "%s cannot be created: %v", path, err)
	}
	return s.entry(placement, placement.abs)
}

// Remove deletes one file, or one directory when it is empty; recursive removal is
// what the caller asks for explicitly.
func (s *Service) Remove(path string, recursive bool) error {
	placement, err := s.resolve(path)
	if err != nil {
		return err
	}
	if placement.abs == placement.root.Path {
		return sessions.Codedf(sessions.CodeBadRequest, "a root cannot be removed")
	}
	info, err := os.Stat(placement.abs)
	if err != nil {
		return notFound(path, err)
	}
	if recursive {
		if err := os.RemoveAll(placement.abs); err != nil {
			return sessions.Codedf(sessions.CodeBadRequest, "%s cannot be removed: %v", path, err)
		}
		return nil
	}
	if info.IsDir() {
		if err := os.Remove(placement.abs); err != nil {
			return sessions.Codedf(sessions.CodeBadRequest,
				"%s is not empty; recursive removal is a separate choice", path)
		}
		return nil
	}
	if err := os.Remove(placement.abs); err != nil {
		return sessions.Codedf(sessions.CodeBadRequest, "%s cannot be removed: %v", path, err)
	}
	return nil
}

// placement is a resolved path together with the root it belongs to.
type placement struct {
	abs  string
	root Root
	rel  string
}

// resolve confines one request path to the roots.
//
// The checks run in this order, and each one matters: an absolute path with `..`
// cleaned away, then symlinks evaluated on the deepest existing ancestor (a write
// target does not exist yet, but its parent must resolve inside the root), then the
// prefix check against the root itself.
func (s *Service) resolve(path string) (placement, error) {
	if strings.TrimSpace(path) == "" {
		return placement{}, sessions.Codedf(sessions.CodeBadRequest, "a path is required")
	}
	if strings.ContainsRune(path, 0) {
		return placement{}, sessions.Codedf(sessions.CodeBadRequest, "the path contains a NUL byte")
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return placement{}, sessions.Codedf(sessions.CodeBadRequest,
			"%s is not an absolute path", path)
	}
	// The literal path is never trusted, not even when it looks like it is under a
	// root: a symlink inside the root can point anywhere, so containment is decided
	// on the resolved form and only then is the operation applied to the path the
	// caller asked for.
	resolved, err := resolveExisting(cleaned)
	if err != nil {
		return placement{}, notFound(path, err)
	}
	for _, root := range s.roots {
		if rel, ok := withinRoot(root.Path, resolved); ok {
			return placement{abs: cleaned, root: root, rel: rel}, nil
		}
	}
	return placement{}, sessions.Codedf("path_escape",
		"%s is outside every allowed workspace", path)
}

// withinRoot reports the relative path when abs is root itself or below it.
//
// The separator matters: /home/user/project-other must not pass for /home/user/project.
func withinRoot(root, abs string) (string, bool) {
	if abs == root {
		return ".", true
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if !strings.HasPrefix(abs, prefix) {
		return "", false
	}
	return strings.TrimPrefix(abs, prefix), true
}

// resolveExisting evaluates the symlinks of the deepest existing ancestor and
// re-appends the missing tail, so a path that is about to be created is checked
// against the same rules as one that exists.
func resolveExisting(abs string) (string, error) {
	if _, err := os.Lstat(abs); err == nil {
		return filepath.EvalSymlinks(abs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return abs, nil
	}
	resolved, err := resolveExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(abs)), nil
}

// entry builds the API view of one path.
func (s *Service) entry(where placement, abs string) (Entry, error) {
	info, err := os.Lstat(abs)
	if err != nil {
		return Entry{}, err
	}
	// A symlink is reported as what it points at: a client editing a file through
	// a link wants the file, and the confinement check already ran on the target.
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(abs)
		if err != nil {
			return Entry{}, err
		}
	}
	rel := where.rel
	if abs != where.abs {
		if child, ok := withinRoot(where.root.Path, abs); ok {
			rel = child
		}
	}
	entry := Entry{
		Name:    info.Name(),
		Path:    abs,
		Rel:     rel,
		RootID:  where.root.ID,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		Mode:    fmt.Sprintf("%04o", info.Mode().Perm()),
		ModTime: info.ModTime().UTC().Format(time.RFC3339),
	}
	if !entry.IsDir && info.Size() <= s.maxRead {
		if sum, err := hashFile(abs); err == nil {
			entry.Sha256 = sum
		}
	}
	return entry, nil
}

// hashFile is the streaming hash of one file.
func hashFile(abs string) (string, error) {
	file, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// notFound maps a syscall failure onto the taxonomy: a missing path is `not_found`,
// anything else stays a readable bad_request.
func notFound(path string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return sessions.Codedf(sessions.CodeNotFound, "%s does not exist", path)
	}
	if errors.Is(err, os.ErrPermission) {
		return sessions.Codedf(sessions.CodeBadRequest, "%s is not readable: %v", path, err)
	}
	return sessions.Codedf(sessions.CodeBadRequest, "%s cannot be read: %v", path, err)
}

// sortEntries orders a listing the way a browser wants to read it: directories
// first, then files, each group by name without case.
func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		left := strings.ToLower(entries[i].Name)
		right := strings.ToLower(entries[j].Name)
		if left == right {
			return entries[i].Name < entries[j].Name
		}
		return left < right
	})
}
