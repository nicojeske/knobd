package focus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderScriptSubstitutesAllPlaceholders(t *testing.T) {
	out, err := renderScript("io.example.svc", "/io/example/svc", "io.example.svc.Iface1")
	if err != nil {
		t.Fatalf("renderScript: %v", err)
	}
	for _, ph := range []string{"@KNOBD_SERVICE@", "@KNOBD_OBJECT@", "@KNOBD_INTERFACE@"} {
		if strings.Contains(out, ph) {
			t.Fatalf("rendered script still contains placeholder %s", ph)
		}
	}
	for _, want := range []string{"io.example.svc", "/io/example/svc", "io.example.svc.Iface1"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered script missing substituted value %q", want)
		}
	}
}

func TestScriptDirUsesXDGRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	got := scriptDir()
	want := "/run/user/1000/knobd"
	if got != want {
		t.Errorf("scriptDir() = %q, want %q", got, want)
	}
}

func TestScriptDirFallsBackWhenXDGRuntimeDirUnset(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	got := scriptDir()
	if !strings.Contains(got, "knobd-") {
		t.Errorf("scriptDir() fallback = %q, want it to contain %q", got, "knobd-")
	}
}

func TestMaterializeScriptWritesReadableFileWithNoPlaceholders(t *testing.T) {
	dir := t.TempDir()
	path, err := materializeScript(dir, "io.example.svc", "/io/example/svc", "io.example.svc.Iface1")
	if err != nil {
		t.Fatalf("materializeScript: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("materializeScript path = %q, want it inside %q", path, dir)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading materialized script: %v", err)
	}
	for _, ph := range []string{"@KNOBD_SERVICE@", "@KNOBD_OBJECT@", "@KNOBD_INTERFACE@"} {
		if strings.Contains(string(data), ph) {
			t.Fatalf("materialized script still contains placeholder %s", ph)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat materialized script: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("materialized script mode = %o, want %o", perm, 0o600)
	}

	// No leftover .tmp file after a successful write.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected no leftover .tmp file, stat err = %v", err)
	}
}

func TestMaterializeScriptIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path1, err := materializeScript(dir, "svc1", "/obj1", "iface1")
	if err != nil {
		t.Fatalf("first materializeScript: %v", err)
	}
	path2, err := materializeScript(dir, "svc2", "/obj2", "iface2")
	if err != nil {
		t.Fatalf("second materializeScript: %v", err)
	}
	if path1 != path2 {
		t.Fatalf("materializeScript paths differ across calls: %q vs %q", path1, path2)
	}
	data, err := os.ReadFile(path2)
	if err != nil {
		t.Fatalf("reading re-materialized script: %v", err)
	}
	if !strings.Contains(string(data), "svc2") {
		t.Error("re-materialized script does not reflect the second call's values")
	}
	if strings.Contains(string(data), "svc1") {
		t.Error("re-materialized script still contains the first call's stale value")
	}
}

func TestMaterializeScriptCreatesDirIfMissing(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "knobd")
	if _, err := materializeScript(dir, "svc", "/obj", "iface"); err != nil {
		t.Fatalf("materializeScript with a missing directory: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected the directory to have been created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
}
