// Package git runs git in a confined workspace.
//
// Every directory is resolved through internal/fs first, so git can only be run
// inside a workspace the operator configured; the commands are `git -C <dir>`, never
// the process's own working directory, so one server can serve many repositories
// without ever changing directory.
package git

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// Defaults for a service nobody tuned.
const (
	DefaultGit     = "git"
	DefaultTimeout = 30 * time.Second
	// MaxDiffBytes bounds what one diff may return; a client that wants the whole
	// thing reads the file.
	MaxDiffBytes = 1 << 20
	maxCommits   = 200
)

// Config wires the service.
type Config struct {
	// FS is the confinement every directory goes through (required).
	FS *fs.Service
	// Git is the binary to run (default DefaultGit), so a test can point at a fake.
	Git string
	// Timeout bounds one git invocation (default DefaultTimeout): a hook that hangs
	// must not hold the request forever.
	Timeout time.Duration
}

// Service is the git seam.
type Service struct {
	fs      *fs.Service
	git     string
	timeout time.Duration
}

// New builds the service.
func New(cfg Config) (*Service, error) {
	if cfg.FS == nil {
		return nil, errors.New("git: the filesystem service is required")
	}
	service := &Service{fs: cfg.FS, git: cfg.Git, timeout: cfg.Timeout}
	if service.git == "" {
		service.git = DefaultGit
	}
	if service.timeout <= 0 {
		service.timeout = DefaultTimeout
	}
	return service, nil
}

// Change is one path git reports as modified, added, deleted or untracked.
type Change struct {
	Path     string `json:"path"`
	Status   string `json:"status"`   // the porcelain code, e.g. "M", "A", "??"
	Staged   bool   `json:"staged"`   // index side of the code
	Unstaged bool   `json:"unstaged"` // worktree side of the code
}

// Status is the working tree of one repository.
type Status struct {
	// Repo is the absolute directory git was run in.
	Repo string `json:"repo"`
	// Branch is the checked-out branch, or "HEAD" when detached.
	Branch string `json:"branch"`
	// Detached is true when no branch is checked out.
	Detached bool `json:"detached"`
	// Ahead and Behind count the commits against the upstream, when there is one.
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
	// Clean is true when there is nothing to stage or commit.
	Clean bool `json:"clean"`
	// Changes is the porcelain listing, staged and unstaged together.
	Changes []Change `json:"changes"`
}

// Commit is one entry of the log.
type Commit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	At      string `json:"at"` // RFC3339
}

// Status reports the working tree of dir.
func (s *Service) Status(ctx context.Context, dir string) (Status, error) {
	repo, err := s.repo(dir)
	if err != nil {
		return Status{}, err
	}
	out, err := s.run(ctx, repo, "status", "--porcelain=v1", "--branch")
	if err != nil {
		return Status{}, err
	}
	status := Status{Repo: repo, Branch: "HEAD", Clean: true, Changes: []Change{}}
	for index, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "## "):
			// `## main...origin/main [ahead 1, behind 2]` or `## HEAD (no branch)`.
			header := strings.TrimPrefix(line, "## ")
			status.Detached = strings.HasPrefix(header, "HEAD (no branch)") ||
				strings.HasPrefix(header, "HEAD detached")
			branch := header
			if cut, _, ok := strings.Cut(header, "..."); ok {
				branch = cut
			}
			if space := strings.IndexByte(branch, ' '); space >= 0 {
				branch = branch[:space]
			}
			if !status.Detached && branch != "" {
				status.Branch = branch
			}
			status.Ahead = countAround(header, "ahead ")
			status.Behind = countAround(header, "behind ")
		case index >= 0 && len(line) >= 3:
			// XY<space>path: X is the index, Y the worktree, `??` untracked.
			status.Changes = append(status.Changes, Change{
				Status:   strings.TrimSpace(line[:2]),
				Path:     strings.TrimSpace(line[3:]),
				Staged:   line[0] != ' ' && line[0] != '?',
				Unstaged: line[1] != ' ' && line[1] != '?',
			})
		}
	}
	status.Clean = len(status.Changes) == 0
	return status, nil
}

// Log returns the newest commits, up to MaxCommits.
func (s *Service) Log(ctx context.Context, dir string, limit int) ([]Commit, error) {
	repo, err := s.repo(dir)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > maxCommits {
		limit = 30
	}
	// A unit separator between the fields and a record separator between commits:
	// a subject may contain anything, including the dashes a prettier format uses.
	out, err := s.run(ctx, repo, "log",
		"--max-count="+strconv.Itoa(limit),
		"--date=iso-strict",
		"--pretty=format:%H%x1f%h%x1f%s%x1f%an%x1f%aI%x1e")
	if err != nil {
		return nil, err
	}
	commits := make([]Commit, 0, limit)
	for _, record := range strings.Split(out, "\x1e") {
		record = strings.Trim(record, "\n")
		if record == "" {
			continue
		}
		fields := strings.Split(record, "\x1f")
		if len(fields) < 5 {
			continue
		}
		commits = append(commits, Commit{
			Hash:    fields[0],
			Short:   fields[1],
			Subject: fields[2],
			Author:  fields[3],
			At:      fields[4],
		})
	}
	return commits, nil
}

// Diff returns the unified diff of dir, or of one path inside it.
func (s *Service) Diff(ctx context.Context, dir, path string, staged bool) (string, error) {
	repo, err := s.repo(dir)
	if err != nil {
		return "", err
	}
	args := []string{"diff", "--no-color"}
	if staged {
		args = append(args, "--cached")
	}
	if strings.TrimSpace(path) != "" {
		if _, err := s.fs.Resolve(path); err != nil {
			return "", err
		}
		args = append(args, "--", path)
	}
	out, err := s.run(ctx, repo, args...)
	if err != nil {
		return "", err
	}
	if len(out) > MaxDiffBytes {
		return out[:MaxDiffBytes], nil
	}
	return out, nil
}

// Stage adds paths to the index (`git add`).
func (s *Service) Stage(ctx context.Context, dir string, paths []string) error {
	repo, err := s.repo(dir)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return sessions.Codedf(sessions.CodeBadRequest, "no path to stage")
	}
	for _, path := range paths {
		if _, err := s.fs.Resolve(path); err != nil {
			return err
		}
	}
	args := append([]string{"add", "--"}, paths...)
	_, err = s.run(ctx, repo, args...)
	return err
}

// Commit records the index with one message and returns the new commit.
func (s *Service) Commit(ctx context.Context, dir, message string) (Commit, error) {
	repo, err := s.repo(dir)
	if err != nil {
		return Commit{}, err
	}
	if strings.TrimSpace(message) == "" {
		return Commit{}, sessions.Codedf(sessions.CodeBadRequest, "a commit needs a message")
	}
	if _, err := s.run(ctx, repo, "commit", "--message", message); err != nil {
		return Commit{}, err
	}
	commits, err := s.Log(ctx, repo, 1)
	if err != nil || len(commits) == 0 {
		return Commit{}, err
	}
	return commits[0], nil
}

// repo confines one directory and checks that git can work there.
func (s *Service) repo(dir string) (string, error) {
	abs, err := s.fs.Resolve(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", sessions.Codedf(sessions.CodeNotFound, "%s is not a directory", dir)
	}
	return abs, nil
}

// run executes one git command in a confined directory and returns its stdout.
//
// A non-zero exit is a bad_request carrying git's own stderr: it is the closest
// thing to an explanation the user can act on ("not a git repository", "nothing to
// commit"), and it never contains a secret of this server.
func (s *Service) run(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	command := exec.CommandContext(ctx, s.git, append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() != nil {
		return "", sessions.Codedf(sessions.CodeTimeout, "git %s timed out", args[0])
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", sessions.Codedf(sessions.CodeBadRequest, "git %s: %s", args[0], message)
	}
	return stdout.String(), nil
}

// countAround reads the number after a marker like `ahead ` in a status header.
func countAround(header, marker string) int {
	index := strings.Index(header, marker)
	if index < 0 {
		return 0
	}
	digits := ""
	for _, char := range header[index+len(marker):] {
		if char < '0' || char > '9' {
			break
		}
		digits += string(char)
	}
	value, _ := strconv.Atoi(digits)
	return value
}
