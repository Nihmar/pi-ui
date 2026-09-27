package sessions

import (
	"strings"
	"testing"
)

func TestWrapBuildsTheContainerInvocation(t *testing.T) {
	isolation, err := NewIsolation("pi-ui:local", "docker",
		[]string{"/opt/pi-ui-bridge", "/run/pi-ui", "/home/u/.pi/agent:/home/u/.pi/agent"},
		"1000:1000", "host")
	if err != nil {
		t.Fatal(err)
	}

	argv := isolation.Wrap(
		[]string{"pi", "--mode", "rpc", "-e", "/opt/pi-ui-bridge/pi-ui-bridge.ts"},
		"/home/u/Projects/app",
		[]string{"PI_UI_BRIDGE_CONFIG=/run/pi-ui/s_1.json"},
	)
	joined := strings.Join(argv, " ")

	// The image and the command, with pi's own flags untouched.
	if !strings.Contains(joined, "--entrypoint pi pi-ui:local --mode rpc") {
		t.Fatalf("the entrypoint and the image must carry the command: %v", argv)
	}
	// Same-path mounts, the working directory first, and a single path expanded to both
	// sides so a typo cannot mount a directory somewhere else.
	for _, want := range []string{
		"--workdir /home/u/Projects/app",
		"src=/home/u/Projects/app,dst=/home/u/Projects/app",
		"src=/opt/pi-ui-bridge,dst=/opt/pi-ui-bridge",
		"src=/run/pi-ui,dst=/run/pi-ui",
		"src=/home/u/.pi/agent,dst=/home/u/.pi/agent",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, argv)
		}
	}
	// The environment travels as --env, because the process spawned is the container CLI.
	if !strings.Contains(joined, "--env PI_UI_BRIDGE_CONFIG=/run/pi-ui/s_1.json") {
		t.Fatalf("the bridge config must reach the container: %v", argv)
	}
	if !strings.Contains(joined, "--rm -i --init --user 1000:1000") {
		t.Fatalf("the container must be disposable and interactive: %v", argv)
	}
}

func TestWrapIsIdentityWithoutIsolation(t *testing.T) {
	var isolation *Isolation
	argv := []string{"pi", "--mode", "rpc"}

	if got := isolation.Wrap(argv, "/work", nil); strings.Join(got, " ") != "pi --mode rpc" {
		t.Fatalf("argv = %v", got)
	}
	if isolation.Describe() != "host" {
		t.Fatalf("describe = %q", isolation.Describe())
	}
	if !strings.Contains((&Isolation{Image: "x", Docker: "docker"}).Describe(), "x") {
		t.Fatal("a container describes its image")
	}
}

func TestWrapKeepsTheWorkingDirectoryFirst(t *testing.T) {
	isolation, err := NewIsolation("pi-ui:local", "docker",
		[]string{"/work", "/elsewhere"}, "", "")
	if err != nil {
		t.Fatal(err)
	}

	argv := isolation.Wrap([]string{"pi"}, "/work", nil)
	workdir := indexIn(argv, "type=bind,src=/work,dst=/work")
	elsewhere := indexIn(argv, "type=bind,src=/elsewhere,dst=/elsewhere")
	if workdir < 0 || elsewhere < 0 {
		t.Fatalf("both mounts must be there: %v", argv)
	}
	if workdir > elsewhere {
		t.Fatalf("the working directory must be mounted first: %v", argv)
	}
	// The default user is this process's, so a bind mount is writable without chmod games.
	if !strings.Contains(strings.Join(argv, " "), "--user ") {
		t.Fatalf("argv = %v", argv)
	}
}

func TestIsolationNeedsAnImage(t *testing.T) {
	if _, err := NewIsolation("", "", nil, "", ""); err == nil {
		t.Fatal("an isolation without an image is a mistake")
	}
	if _, err := NewIsolation("pi-ui:local", "", nil, "", "sneaky"); err == nil {
		t.Fatal("an unknown network mode must be refused, not passed on")
	}
	isolation, err := NewIsolation("pi-ui:local", "", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if isolation.Docker != "docker" {
		t.Fatalf("docker = %q", isolation.Docker)
	}
	if !strings.Contains(isolation.User, ":") {
		t.Fatalf("user = %q", isolation.User)
	}
	// Host by default: a session usually talks to a model server on the machine, and those
	// bind loopback.
	if isolation.Network != "host" {
		t.Fatalf("network = %q", isolation.Network)
	}
}

func TestTheNetworkModeReachesTheCommandLine(t *testing.T) {
	for _, mode := range []string{"host", "none", "bridge"} {
		isolation, err := NewIsolation("pi-ui:local", "docker", nil, "1000:1000", mode)
		if err != nil {
			t.Fatal(err)
		}
		argv := isolation.Wrap([]string{"pi"}, "/work", nil)
		joined := strings.Join(argv, " ")
		if !strings.Contains(joined, "--network "+mode) {
			t.Fatalf("%s: %v", mode, argv)
		}
		if !strings.Contains(isolation.Describe(), mode) {
			t.Fatalf("%s is not in %q", mode, isolation.Describe())
		}
	}
}

func TestTheRuntimeMountIsOnlyNeededWithTheBridge(t *testing.T) {
	if !NeedsRuntimeMount([]string{"pi", "-e", "/x/pi-ui-bridge.ts"}) {
		t.Fatal("a child that loads the bridge reads the runtime file")
	}
	if NeedsRuntimeMount([]string{"pi", "--mode", "rpc"}) {
		t.Fatal("a child without the bridge does not")
	}
}

// indexIn returns the position of the first element equal to want, or -1.
func indexIn(argv []string, want string) int {
	for index, element := range argv {
		if element == want {
			return index
		}
	}
	return -1
}
