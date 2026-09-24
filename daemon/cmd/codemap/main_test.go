package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerate runs the generator against the real daemon module root
// (this test lives inside it) and checks that a handful of known,
// stable signatures come out correctly. It's a smoke test against
// content drift in the generator, not a substitute for reviewing
// docs/codemap.md itself when a change touches this package.
func TestGenerate(t *testing.T) {
	root := findModuleRoot(t)

	pkgs, err := loadPackages(root)
	if err != nil {
		t.Fatalf("loadPackages: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("loadPackages returned no packages")
	}

	// Packages must be sorted by import path (determinism).
	for i := 1; i < len(pkgs); i++ {
		if pkgs[i-1].importPath >= pkgs[i].importPath {
			t.Fatalf("packages not sorted: %q before %q", pkgs[i-1].importPath, pkgs[i].importPath)
		}
	}

	// codemap must not document itself.
	for _, p := range pkgs {
		if p.importPath == "cmd/codemap" {
			t.Fatal("loadPackages included cmd/codemap, which should be skipped")
		}
	}

	var midi *pkgInfo
	for _, p := range pkgs {
		if p.importPath == "internal/midi" {
			midi = p
		}
	}
	if midi == nil {
		t.Fatal("internal/midi not found")
	}

	var buf bytes.Buffer
	writePackage(&buf, midi)
	got := buf.String()

	for _, want := range []string{
		"### `Port` (interface)",
		"Read(ctx context.Context) (Message, error)",
		"Write(ctx context.Context, msg Message) error",
		"Close() error",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("internal/midi codemap missing %q\ngot:\n%s", want, got)
		}
	}
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test working directory")
		}
		dir = parent
	}
}
