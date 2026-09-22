// Package proctree walks Linux process ancestry via /proc, for
// matching a focused window's pid against an audio stream's pid when
// they belong to the same application but are different processes (a
// browser's focused-window pid is often a parent/grandparent of the
// pid that actually owns its audio stream — see
// specs/milestones/M06-focus-tracking.md).
//
// This is deliberately a separate package from daemon/internal/audio,
// which already has its own single-hop /proc/<pid>/exe lookup
// (AnnotateProcessBinaries, for a different purpose: filling in a
// missing application.process.binary). The two are not merged: they
// read different things (/proc/<pid>/stat's ppid/starttime here, vs.
// /proc/<pid>/exe there) for different reasons, and the ~3 lines of
// duplication in reading a symlink is cheaper than coupling working,
// tested M03 code to a new M06 package.
package proctree

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultMaxDepth bounds every ancestor walk. A browser's process tree
// is typically 2-4 hops deep (confirmed live against Brave: window pid
// -> ... -> the pid owning its audio stream); a Flatpak/bubblewrap or
// systemd-scope wrapper adds a few more. 16 is comfortably past
// anything real, and still bounds a walk through a corrupted or
// adversarial /proc.
const DefaultMaxDepth = 16

// Walker reads process relationships out of a /proc-shaped directory
// tree. Root is normally left empty (meaning "/proc"); tests set it to
// a t.TempDir() containing fabricated <pid>/stat files and <pid>/exe
// symlinks, mirroring the pattern daemon/internal/audio's
// AnnotateProcessBinaries tests already use for a fake /proc/<pid>/exe.
type Walker struct {
	Root string
}

func (w Walker) root() string {
	if w.Root == "" {
		return "/proc"
	}
	return w.Root
}

// Ancestors returns pid followed by each of its ancestors, walking
// upward via /proc/<pid>/stat's ppid field, stopping after maxDepth
// hops or at pid 1 (init), whichever comes first.
//
// It never returns an error: a missing, unreadable, or malformed
// /proc/<pid>/stat simply ends the chain at that point, the same
// philosophy as AnnotateProcessBinaries's swallowed os.Readlink
// errors — the pid may have exited between whatever enumerated it and
// this call, which is normal, not exceptional.
//
// A pid-reuse guard is applied while walking: process start times
// (also read from stat) must be non-decreasing from child to parent,
// since a real parent has always started at or before its child. If a
// claimed parent's start time is later than its child's, the pid has
// been recycled out from under the walk, and the chain is truncated
// there rather than risk following an unrelated process upward.
func (w Walker) Ancestors(pid, maxDepth int) []int {
	if pid <= 0 || maxDepth <= 0 {
		return nil
	}
	out := make([]int, 0, maxDepth+1)
	out = append(out, pid)

	// cur's own (ppid, starttime), read once here and thereafter
	// carried forward from the previous hop's read of the new cur --
	// exactly one /proc read per hop, not two.
	cur := pid
	ppid, curStart, ok := w.parent(cur)
	if !ok {
		return out
	}
	for hop := 0; hop < maxDepth; hop++ {
		if ppid <= 0 || ppid == cur {
			return out
		}
		// nextPPID/parentStart are ppid's OWN ppid and starttime.
		nextPPID, parentStart, ok := w.parent(ppid)
		if !ok {
			return out
		}
		if parentStart > curStart {
			// Reuse guard: a real parent cannot have started after
			// its child. Truncate rather than trust this hop.
			return out
		}
		out = append(out, ppid)
		if ppid == 1 {
			// init: a legitimate final ancestor, but nothing above it
			// is interesting.
			return out
		}
		cur, curStart, ppid = ppid, parentStart, nextPPID
	}
	return out
}

// IsRelated reports whether a is an ancestor-or-self of b, or b of a,
// within maxDepth hops in either direction. It deliberately does not
// match siblings or cousins sharing a more distant common ancestor:
// under a typical systemd --user session, nearly every process shares
// *some* ancestor, so any "common ancestor" rule would be a
// false-positive generator rather than useful evidence. See
// daemon/internal/audio/focus.go's doc comment for how this is used
// (as a fallback rung, only once name-based matching has failed).
func (w Walker) IsRelated(a, b, maxDepth int) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	if a == b {
		return true
	}
	for _, p := range w.Ancestors(a, maxDepth) {
		if p == b {
			return true
		}
	}
	for _, p := range w.Ancestors(b, maxDepth) {
		if p == a {
			return true
		}
	}
	return false
}

// ExeName returns the base name of /proc/<pid>/exe's target, or "" if
// pid doesn't exist, the symlink can't be read, or it resolves to
// nothing usable.
func (w Walker) ExeName(pid int) string {
	if pid <= 0 {
		return ""
	}
	exe, err := os.Readlink(filepath.Join(w.root(), strconv.Itoa(pid), "exe"))
	if err != nil {
		return ""
	}
	base := filepath.Base(exe)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	return base
}

// parent reads /proc/<pid>/stat and returns its ppid and starttime
// (fields 4 and 22 in man proc(5)'s numbering), or ok=false if the
// file is missing, unreadable, or malformed.
//
// comm (field 2) is parenthesized and may itself contain spaces and
// even parentheses, so the fields cannot be split with a plain
// strings.Fields(line)[n] -- that misindexes every field after comm
// for a process whose name contains a space. Instead, split at the
// *last* ')' in the line (comm cannot contain a ')' followed by
// nothing else, since the kernel always closes it as the final
// character before " <state>"), and count fields from there: index 0
// is state (field 3), so field N is at index N-3.
func (w Walker) parent(pid int) (ppid int, starttime uint64, ok bool) {
	data, err := os.ReadFile(filepath.Join(w.root(), strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, 0, false
	}
	i := bytes.LastIndexByte(data, ')')
	if i < 0 || i+1 >= len(data) {
		return 0, 0, false
	}
	fields := strings.Fields(string(data[i+1:]))
	const (
		ppidIdx      = 1  // field 4
		starttimeIdx = 19 // field 22
	)
	if len(fields) <= starttimeIdx {
		return 0, 0, false
	}
	ppid64, err := strconv.Atoi(fields[ppidIdx])
	if err != nil {
		return 0, 0, false
	}
	start, err := strconv.ParseUint(fields[starttimeIdx], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return ppid64, start, true
}
