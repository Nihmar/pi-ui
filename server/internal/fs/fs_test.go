package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// newService builds a service over a temporary workspace with one file and one
// directory in it.
func newService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	return service, root
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a failure")
	}
	return sessions.CodeOf(err)
}

func TestRootsAreResolvedAndListed(t *testing.T) {
	service, root := newService(t)

	roots := service.Roots()
	if len(roots) != 1 {
		t.Fatalf("expected one root, got %d", len(roots))
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if roots[0].Path != resolved {
		t.Fatalf("root %q is not the resolved %q", roots[0].Path, resolved)
	}
	if roots[0].ID != filepath.Base(resolved) {
		t.Fatalf("root id %q is not the base name", roots[0].ID)
	}
}

func TestNewRejectsAnImpossibleRoot(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("a service without roots must not build")
	}
	if _, err := New(Config{Roots: []string{filepath.Join(t.TempDir(), "missing")}}); err == nil {
		t.Fatal("a missing root must not build")
	}
}

func TestListPutsDirectoriesFirst(t *testing.T) {
	service, root := newService(t)

	entries, err := service.List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected two entries, got %d", len(entries))
	}
	if !entries[0].IsDir || entries[0].Name != "sub" {
		t.Fatalf("the directory should come first, got %+v", entries[0])
	}
	if entries[1].Name != "notes.md" || entries[1].Sha256 == "" {
		t.Fatalf("the file should come second with a hash, got %+v", entries[1])
	}
	if entries[1].Rel != "notes.md" || entries[1].RootID != filepath.Base(root) {
		t.Fatalf("the entry is not relative to its root: %+v", entries[1])
	}
}

func TestReadWriteRoundTrip(t *testing.T) {
	service, root := newService(t)
	path := filepath.Join(root, "notes.md")

	written, err := service.Write(path, []byte("second"), "")
	if err != nil {
		t.Fatal(err)
	}
	if written.Size != 6 {
		t.Fatalf("size %d", written.Size)
	}
	data, entry, err := service.Read(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second" || entry.Sha256 != written.Sha256 {
		t.Fatalf("read back %q (%s)", data, entry.Sha256)
	}
}

func TestReadRefusesWhatIsAboveTheCap(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "big.bin")
	if err := os.WriteFile(path, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{
		Roots:         []string{root},
		MaxReadBytes:  1024,
		MaxWriteBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Read(path, 0)
	if code := codeOf(t, err); code != sessions.CodeTooLarge {
		t.Fatalf("code %q", code)
	}
	if _, err := service.Write(path, make([]byte, 2048), ""); err == nil {
		t.Fatal("the write cap must refuse 2048 bytes")
	}
}

func TestConditionalWriteDetectsAChange(t *testing.T) {
	service, root := newService(t)
	path := filepath.Join(root, "notes.md")

	entry, err := service.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Write(path, []byte("mine"), entry.Sha256); err != nil {
		t.Fatalf("the first conditional write should pass: %v", err)
	}
	if _, err := service.Write(path, []byte("stale"), entry.Sha256); err == nil {
		t.Fatal("a stale hash must not overwrite the file")
	} else if code := sessions.CodeOf(err); code != sessions.CodeBadRequest {
		t.Fatalf("code %q", code)
	}
	if _, err := service.Write(filepath.Join(root, "new.md"), []byte("x"), "deadbeef"); err == nil {
		t.Fatal("a conditional write must not create a file")
	}
}

func TestPathsOutsideEveryRootAreRefused(t *testing.T) {
	service, root := newService(t)

	outside := filepath.Join(root, "..", "elsewhere")
	_, err := service.List(outside)
	if code := codeOf(t, err); code != "path_escape" {
		t.Fatalf("expected path_escape, got %q", code)
	}
	if _, err := service.Stat("/etc"); err == nil {
		t.Fatal("/etc must not be reachable")
	}
	if _, err := service.Write(outside, []byte("x"), ""); err == nil {
		t.Fatal("a write outside the roots must fail")
	}
}

func TestASymlinkCannotLeaveTheRoot(t *testing.T) {
	service, root := newService(t)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("classified"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, _, err := service.Read(link, 0); err == nil {
		t.Fatal("reading through an escaping symlink must fail")
	} else if code := sessions.CodeOf(err); code != "path_escape" {
		t.Fatalf("expected path_escape, got %q", code)
	}
}

func TestALinkInsideTheRootIsFollowable(t *testing.T) {
	service, root := newService(t)
	link := filepath.Join(root, "alias.md")
	if err := os.Symlink(filepath.Join(root, "notes.md"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	entry, err := service.Stat(link)
	if err != nil {
		t.Fatalf("a link inside the root should resolve: %v", err)
	}
	if entry.IsDir || entry.Size != 5 {
		t.Fatalf("the link should report the target: %+v", entry)
	}
}

func TestRemoveIsExplicit(t *testing.T) {
	service, root := newService(t)
	sub := filepath.Join(root, "sub")
	if err := os.WriteFile(filepath.Join(sub, "child.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := service.Remove(sub, false); err == nil {
		t.Fatal("a non-empty directory must not go without recursive")
	}
	if err := service.Remove(root, true); err == nil {
		t.Fatal("a root must never be removed")
	}
	if err := service.Remove(sub, true); err != nil {
		t.Fatalf("recursive removal: %v", err)
	}
	if _, err := os.Stat(sub); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the directory should be gone")
	}
}

func TestMkdirCreatesParents(t *testing.T) {
	service, root := newService(t)
	deep := filepath.Join(root, "a", "b", "c")

	entry, err := service.Mkdir(deep)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.IsDir || !strings.HasSuffix(entry.Path, filepath.Join("a", "b", "c")) {
		t.Fatalf("unexpected entry %+v", entry)
	}
}

func TestResolveIsThePublishedConfinement(t *testing.T) {
	service, root := newService(t)
	path := filepath.Join(root, "sub", "created", "later.md")

	resolved, err := service.Resolve(path)
	if err != nil {
		t.Fatalf("a path under the root must resolve, even before it exists: %v", err)
	}
	if resolved != path {
		t.Fatalf("resolved %q, wanted %q", resolved, path)
	}
	if _, err := service.Resolve(filepath.Join(root, "..", "outside")); err == nil {
		t.Fatal("an escape must not resolve")
	} else if code := sessions.CodeOf(err); code != "path_escape" {
		t.Fatalf("code %q", code)
	}
}

func TestBadPathsAreBadRequests(t *testing.T) {
	service, root := newService(t)

	for _, path := range []string{"", "  ", "relative/path", "/tmp/x\x00y"} {
		if _, err := service.Stat(path); err == nil {
			t.Fatalf("%q should fail", path)
		} else if code := sessions.CodeOf(err); code != sessions.CodeBadRequest {
			t.Fatalf("%q: code %q", path, code)
		}
	}
	if _, err := service.Stat(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a missing path must fail")
	} else if code := sessions.CodeOf(err); code != sessions.CodeNotFound {
		t.Fatalf("missing path: code %q", code)
	}
}
