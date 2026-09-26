package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// newService builds a service over a file that does not exist yet.
func newService(t *testing.T) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp.json")
	return New(path), path
}

func TestAMissingFileIsAnEmptyCatalogue(t *testing.T) {
	service, _ := newService(t)

	document, err := service.Read()
	if err != nil {
		t.Fatalf("a missing file is not an error: %v", err)
	}
	if len(document.Servers) != 0 || len(document.EnabledNames()) != 0 {
		t.Fatalf("document = %+v", document)
	}
	if service.Path() == "" || !service.Configured() {
		t.Fatal("the path is part of the service")
	}
}

func TestWriteCreatesTheFileAndReadsItBack(t *testing.T) {
	service, path := newService(t)

	written, err := service.Write(Document{Servers: map[string]Server{
		"files":  {Command: "npx", Args: []string{"-y", "server-filesystem", "/srv"}, Env: map[string]string{"TOKEN": "s3cret"}},
		"remote": {URL: "https://mcp.example/sse", Headers: map[string]string{"Authorization": "Bearer abc"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// What a reader gets never carries a secret.
	if written.Servers["files"].Env["TOKEN"] != Redacted {
		t.Fatalf("env value leaked: %+v", written.Servers["files"].Env)
	}
	if written.Servers["remote"].Headers["Authorization"] != Redacted {
		t.Fatalf("header value leaked: %+v", written.Servers["remote"].Headers)
	}
	if names := written.EnabledNames(); len(names) != 2 {
		t.Fatalf("enabled = %v", names)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "s3cret") {
		t.Fatalf("the file must hold the real value: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("a temporary file was left behind: %s", entry.Name())
		}
	}
}

func TestTheSentinelKeepsTheStoredSecret(t *testing.T) {
	service, _ := newService(t)

	if _, err := service.Write(Document{Servers: map[string]Server{
		"files": {Command: "npx", Env: map[string]string{"TOKEN": "s3cret", "OTHER": "keep"}},
	}}); err != nil {
		t.Fatal(err)
	}
	// A client reads the redacted document and writes it back with one change.
	read, err := service.Read()
	if err != nil {
		t.Fatal(err)
	}
	server := read.Servers["files"]
	server.Args = []string{"--verbose"}
	read.Servers["files"] = server
	if _, err := service.Write(read); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(service.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "s3cret") {
		t.Fatalf("the stored secret was lost: %s", raw)
	}
	if !strings.Contains(string(raw), "keep") {
		t.Fatalf("the second value was lost: %s", raw)
	}
	if !strings.Contains(string(raw), "--verbose") {
		t.Fatalf("the change was not stored: %s", raw)
	}
}

func TestASentinelWithoutAStoredValueIsRefused(t *testing.T) {
	service, _ := newService(t)

	_, err := service.Write(Document{Servers: map[string]Server{
		"files": {Command: "npx", Env: map[string]string{"TOKEN": Redacted}},
	}})
	if code := sessions.CodeOf(err); code != sessions.CodeBadRequest {
		t.Fatalf("expected bad_request, got %q (%v)", code, err)
	}
}

func TestUnknownFieldsSurviveARoundTrip(t *testing.T) {
	service, path := newService(t)
	raw := `{
	  "version": 2,
	  "futureTop": {"a": 1},
	  "servers": {
	    "files": {"command": "npx", "timeoutSec": 30, "env": {"TOKEN": "s3cret"}}
	  }
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	document, err := service.Read()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Write(document); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"futureTop", "timeoutSec", "s3cret", `"version": 2`} {
		if !strings.Contains(string(written), want) {
			t.Fatalf("%q was lost: %s", want, written)
		}
	}
}

func TestValidation(t *testing.T) {
	service, _ := newService(t)

	cases := map[string]Server{
		"nothing":     {},
		"both":        {Command: "npx", URL: "https://x.example"},
		"badurl":      {URL: "ftp://x.example"},
		"emptyheader": {URL: "https://x.example", Headers: map[string]string{"": "v"}},
		"nul":         {Command: "npx", Env: map[string]string{"A": "a\x00b"}},
	}
	for name, server := range cases {
		_, err := service.Write(Document{Servers: map[string]Server{name: server}})
		if err == nil {
			t.Fatalf("%s should be refused", name)
		}
		if code := sessions.CodeOf(err); code != sessions.CodeBadRequest {
			t.Fatalf("%s: code %q", name, code)
		}
	}
	if _, err := service.Write(Document{Servers: map[string]Server{" ": {Command: "npx"}}}); err == nil {
		t.Fatal("a server needs a name")
	}
	tooMany := map[string]Server{}
	for index := 0; index < maxServers+1; index++ {
		tooMany[string(rune('a'+index%26))+string(rune('A'+index/26))] = Server{Command: "x"}
	}
	if _, err := service.Write(Document{Servers: tooMany}); err == nil {
		t.Fatal("the catalogue is bounded")
	}
}

func TestDisabledServersAreNotEnabled(t *testing.T) {
	service, _ := newService(t)

	disabled := false
	document, err := service.Write(Document{Servers: map[string]Server{
		"on":  {Command: "npx"},
		"off": {Command: "npx", Enabled: &disabled},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if names := document.EnabledNames(); len(names) != 1 || names[0] != "on" {
		t.Fatalf("enabled = %v", names)
	}
	if !document.Servers["off"].Disabled() {
		t.Fatal("the flag should survive a round trip")
	}
}

func TestAFileThatIsNotAnObjectIsRefused(t *testing.T) {
	service, path := newService(t)
	if err := os.WriteFile(path, []byte("[1,2]"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := service.Read()
	if code := sessions.CodeOf(err); code != sessions.CodeBadRequest {
		t.Fatalf("expected bad_request, got %q (%v)", code, err)
	}
}

func TestAnUnconfiguredServiceSaysSo(t *testing.T) {
	service := New("")
	if service.Configured() {
		t.Fatal("an empty path is not configured")
	}
	if _, err := service.Read(); err == nil {
		t.Fatal("reading without a path must fail")
	} else if code := sessions.CodeOf(err); code != sessions.CodeUnsupported {
		t.Fatalf("code = %q", code)
	}
	if _, err := service.Write(Document{}); err == nil {
		t.Fatal("writing without a path must fail")
	}
	if _, err := json.Marshal(Document{}); err != nil {
		t.Fatal(err)
	}
	_ = time.Now()
}
