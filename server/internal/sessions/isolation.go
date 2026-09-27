package sessions

import (
	"fmt"
	"strings"
)

// Isolation runs every child inside its own container instead of on the host.
//
// It is a wrapper around the argv a child would otherwise get: `pi` and its flags are
// unchanged, only the process that executes them changes. That is deliberate — the
// alternative (teaching the supervisor about containers) would put a second launcher in the
// middle of the lifecycle, and the lifecycle is what the tests pin.
//
// What the container needs is what the argv already refers to: the working directory, the
// bridge extension, the runtime file the bridge config lives in, and the pi configuration
// with the credentials. Each one is mounted **same-path**, because a session's working
// directory is its identity: a path that reads /home/you/project inside the container must
// read exactly that outside, or every path pi reports is about a tree nobody asked for.
type Isolation struct {
	// Image is the container image that carries pi (deploy/Dockerfile builds one).
	Image string
	// Docker is the container CLI (default "docker"), so a test can point at a script.
	Docker string
	// Mounts are host:container paths, both written the same way. A bind mount of a path
	// that does not exist fails the run, which is the honest place to notice a typo.
	Mounts []string
	// User is the uid:gid the container runs as (default: this process's).
	User string
}

// NewIsolation validates an isolation configuration.
func NewIsolation(image, docker string, mounts []string, user string) (*Isolation, error) {
	if strings.TrimSpace(image) == "" {
		return nil, fmt.Errorf("isolation needs an image")
	}
	if docker == "" {
		docker = "docker"
	}
	if user == "" {
		user = defaultIsolationUser()
	}
	clean := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		entry := strings.TrimSpace(mount)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, ":") {
			// A single path means "same path on both sides", which is the rule this whole
			// feature exists for.
			entry += ":" + entry
		}
		clean = append(clean, entry)
	}
	return &Isolation{Image: image, Docker: docker, Mounts: clean, User: user}, nil
}

// Wrap turns the pi argv of one child into the container invocation that runs it.
//
// The environment travels as `--env` entries rather than as the child's environment: the
// process this server spawns is the container CLI, which does not need the session's
// variables, and the container does.
func (i *Isolation) Wrap(argv []string, cwd string, env []string) []string {
	if i == nil || len(argv) == 0 {
		return argv
	}
	wrapped := make([]string, 0, len(argv)+len(i.Mounts)*2+len(env)*2+10)
	wrapped = append(wrapped,
		i.Docker, "run", "--rm", "-i", "--init",
		"--user", i.User,
	)
	// The working directory first, so a `--mount` the operator adds can never shadow it.
	if cwd != "" {
		wrapped = append(wrapped, "--workdir", cwd, "--mount", "type=bind,src="+cwd+",dst="+cwd)
	}
	for _, mount := range i.Mounts {
		source, target, _ := strings.Cut(mount, ":")
		if cwd != "" && source == cwd {
			continue
		}
		wrapped = append(wrapped, "--mount", "type=bind,src="+source+",dst="+target)
	}
	for _, entry := range env {
		if entry == "" {
			continue
		}
		wrapped = append(wrapped, "--env", entry)
	}
	wrapped = append(wrapped, "--entrypoint", argv[0], i.Image)
	return append(wrapped, argv[1:]...)
}

// Describe renders the isolation for a log line.
func (i *Isolation) Describe() string {
	if i == nil {
		return "host"
	}
	return fmt.Sprintf("container %s (%d mount(s), user %s)", i.Image, len(i.Mounts), i.User)
}

// NeedsRuntimeMount reports whether the runtime directory must be mounted for one child:
// it holds the bridge configuration, and the bridge reads it from inside the container.
func NeedsRuntimeMount(argv []string) bool {
	return hasFlag(argv, flagExtension)
}
