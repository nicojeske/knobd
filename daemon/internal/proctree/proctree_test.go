package proctree

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeStat fabricates a /proc/<pid>/stat file under root with the
// given ppid and starttime, mirroring the t.TempDir()-based fake /proc
// pattern daemon/internal/audio's pulse_test.go already uses for
// /proc/<pid>/exe. comm is written parenthesized exactly as the kernel
// does, so a test can smuggle spaces/parens into it.
func writeStat(t *testing.T, root string, pid, ppid int, comm string, starttime uint64) {
	t.Helper()
	// fields[0..18] are man proc(5) fields 4..22 (ppid..starttime);
	// only ppid (index 0) and starttime (index 18) carry real values,
	// the rest are zero filler of no interest to the parser.
	fields := make([]string, 19)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = strconv.Itoa(ppid)
	fields[18] = strconv.FormatUint(starttime, 10)
	line := fmt.Sprintf("%d (%s) R %s\n", pid, comm, strings.Join(fields, " "))

	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(line), 0o644); err != nil {
		t.Fatalf("write stat: %v", err)
	}
}

func TestAncestorsDirectParent(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 200, 100, "child", 50)
	writeStat(t, root, 100, 1, "parent", 10)
	writeStat(t, root, 1, 0, "init", 1)

	w := Walker{Root: root}
	got := w.Ancestors(200, DefaultMaxDepth)
	want := []int{200, 100, 1}
	if !equalInts(got, want) {
		t.Fatalf("Ancestors(200) = %v, want %v", got, want)
	}
}

func TestAncestorsBraveShapedChain(t *testing.T) {
	// Mirrors the real dev-machine shape: a window pid several hops
	// above the pid that actually owns the audio stream.
	root := t.TempDir()
	writeStat(t, root, 4417, 3999, "brave-audio", 500) // stream pid
	writeStat(t, root, 3999, 3214, "brave-child", 400)
	writeStat(t, root, 3214, 3172, "brave-mid", 300)
	writeStat(t, root, 3172, 1712, "brave", 100) // window pid
	writeStat(t, root, 1712, 1, "systemd-user", 10)
	writeStat(t, root, 1, 0, "init", 1)

	w := Walker{Root: root}
	if !w.IsRelated(4417, 3172, DefaultMaxDepth) {
		t.Fatal("expected stream pid 4417 to be related to window pid 3172 via ancestry")
	}
	if !w.IsRelated(3172, 4417, DefaultMaxDepth) {
		t.Fatal("IsRelated should be symmetric")
	}
}

func TestAncestorsDeeperThanMaxDepthDoesNotMatch(t *testing.T) {
	root := t.TempDir()
	// A chain of 5 hops from 600 up to 100.
	writeStat(t, root, 600, 500, "p600", 60)
	writeStat(t, root, 500, 400, "p500", 50)
	writeStat(t, root, 400, 300, "p400", 40)
	writeStat(t, root, 300, 200, "p300", 30)
	writeStat(t, root, 200, 100, "p200", 20)
	writeStat(t, root, 100, 1, "p100", 10)

	w := Walker{Root: root}
	if w.IsRelated(600, 100, 2) {
		t.Fatal("expected pid 100 to be out of reach at maxDepth=2")
	}
	if !w.IsRelated(600, 100, DefaultMaxDepth) {
		t.Fatal("expected pid 100 to be reachable at DefaultMaxDepth")
	}
}

func TestAncestorsMissingHopTruncatesChain(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 300, 200, "child", 30)
	// 200's stat file is deliberately absent -- it exited between
	// enumeration and this call, or /proc raced us.
	writeStat(t, root, 100, 1, "grandparent", 10)

	w := Walker{Root: root}
	got := w.Ancestors(300, DefaultMaxDepth)
	want := []int{300}
	if !equalInts(got, want) {
		t.Fatalf("Ancestors(300) = %v, want %v (chain should truncate at the missing hop)", got, want)
	}
}

// TestAncestorsCommWithSpacesAndParensParsesCorrectly pins the reason
// parent() splits at the *last* ')' rather than using
// strings.Fields(line)[n]: comm can itself contain spaces and
// parentheses, which would misindex every field after it under a naive
// split.
func TestAncestorsCommWithSpacesAndParensParsesCorrectly(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 300, 200, "weird (name) here", 30)
	writeStat(t, root, 200, 1, "parent", 10)
	writeStat(t, root, 1, 0, "init", 1)

	w := Walker{Root: root}
	got := w.Ancestors(300, DefaultMaxDepth)
	want := []int{300, 200, 1}
	if !equalInts(got, want) {
		t.Fatalf("Ancestors(300) = %v, want %v (comm with spaces/parens broke ppid parsing)", got, want)
	}
}

func TestAncestorsStopsAtPPIDZeroOrOne(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 1, 0, "init", 1)

	w := Walker{Root: root}
	got := w.Ancestors(1, DefaultMaxDepth)
	want := []int{1}
	if !equalInts(got, want) {
		t.Fatalf("Ancestors(1) = %v, want %v", got, want)
	}
}

func TestAncestorsStopsOnSelfParent(t *testing.T) {
	root := t.TempDir()
	// A pathological/corrupted entry claiming to be its own parent.
	writeStat(t, root, 500, 500, "weird", 10)

	w := Walker{Root: root}
	got := w.Ancestors(500, DefaultMaxDepth)
	want := []int{500}
	if !equalInts(got, want) {
		t.Fatalf("Ancestors(500) = %v, want %v (self-parent must not loop)", got, want)
	}
}

func TestAncestorsStarttimeInversionTruncatesChain(t *testing.T) {
	root := t.TempDir()
	// 200 claims to be 300's parent, but started AFTER 300 did -- a
	// telltale sign the pid was recycled underneath the walk.
	writeStat(t, root, 300, 200, "child", 100)
	writeStat(t, root, 200, 1, "recycled-parent", 999)

	w := Walker{Root: root}
	got := w.Ancestors(300, DefaultMaxDepth)
	want := []int{300}
	if !equalInts(got, want) {
		t.Fatalf("Ancestors(300) = %v, want %v (starttime inversion should truncate the chain)", got, want)
	}
}

func TestIsRelatedSelfAndUnrelated(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 100, 1, "a", 10)
	writeStat(t, root, 200, 1, "b", 20)
	writeStat(t, root, 1, 0, "init", 1)

	w := Walker{Root: root}
	if !w.IsRelated(100, 100, DefaultMaxDepth) {
		t.Fatal("IsRelated(pid, pid) should be true")
	}
	if w.IsRelated(100, 200, DefaultMaxDepth) {
		t.Fatal("100 and 200 only share init as a common ancestor; IsRelated must not treat that as related")
	}
	if w.IsRelated(0, 100, DefaultMaxDepth) || w.IsRelated(100, -1, DefaultMaxDepth) {
		t.Fatal("IsRelated with a non-positive pid must be false")
	}
}

func TestExeName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "1234")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/usr/bin/vesktop", filepath.Join(dir, "exe")); err != nil {
		t.Fatal(err)
	}

	w := Walker{Root: root}
	if got := w.ExeName(1234); got != "vesktop" {
		t.Fatalf("ExeName(1234) = %q, want %q", got, "vesktop")
	}
	if got := w.ExeName(9999); got != "" {
		t.Fatalf("ExeName(9999) (no such pid) = %q, want empty", got)
	}

	brokenDir := filepath.Join(root, "5555")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent/path/does-not-matter", filepath.Join(brokenDir, "exe")); err != nil {
		t.Fatal(err)
	}
	// os.Readlink succeeds even for a broken symlink (it doesn't follow
	// the link); ExeName should still return the target's base name.
	if got := w.ExeName(5555); got != "does-not-matter" {
		t.Fatalf("ExeName(5555) (broken symlink) = %q, want %q", got, "does-not-matter")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
