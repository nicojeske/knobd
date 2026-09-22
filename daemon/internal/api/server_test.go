package api

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestSocketPath(t *testing.T) {
	t.Run("uses XDG_RUNTIME_DIR when set", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
		got, err := SocketPath()
		if err != nil {
			t.Fatalf("SocketPath: %v", err)
		}
		want := "/run/user/1000/knobd.sock"
		if got != want {
			t.Errorf("SocketPath() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to TMPDIR when unset", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "")
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		got, err := SocketPath()
		if err != nil {
			t.Fatalf("SocketPath: %v", err)
		}
		want := filepath.Join(tmp, "knobd-"+strconv.Itoa(os.Getuid())+".sock")
		if got != want {
			t.Errorf("SocketPath() = %q, want %q", got, want)
		}
	})
}

// shortSocketDir returns a temp directory short enough that a filename
// appended to it stays well under sun_path's ~107 byte limit -- t.TempDir()
// embeds the test's (sub)name, which for a table-driven test can already
// eat most of that budget on its own.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "knobd-api-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestListen(t *testing.T) {
	t.Run("binds cleanly with nothing at path", func(t *testing.T) {
		path := filepath.Join(shortSocketDir(t), "s.sock")
		ln, err := listen(path)
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer ln.Close()
		if info, err := os.Stat(path); err != nil || info.Mode()&os.ModeSocket == 0 {
			t.Errorf("expected a socket at %s", path)
		}
	})

	t.Run("rebinds over a stale socket", func(t *testing.T) {
		path := filepath.Join(shortSocketDir(t), "s.sock")
		stale, err := net.Listen("unix", path)
		if err != nil {
			t.Fatalf("create stale socket: %v", err)
		}
		// Leave the file behind on Close, exactly as a crashed daemon
		// would (it never gets a chance to Close its own listener).
		stale.(*net.UnixListener).SetUnlinkOnClose(false)
		stale.Close()

		ln, err := listen(path)
		if err != nil {
			t.Fatalf("listen over a stale socket: %v", err)
		}
		defer ln.Close()
	})

	t.Run("refuses to bind over a live listener", func(t *testing.T) {
		path := filepath.Join(shortSocketDir(t), "s.sock")
		live, err := net.Listen("unix", path)
		if err != nil {
			t.Fatalf("create live socket: %v", err)
		}
		defer live.Close()

		_, err = listen(path)
		if err == nil {
			t.Fatal("expected an error binding over a live listener")
		}
	})

	t.Run("refuses to unlink a regular file", func(t *testing.T) {
		path := filepath.Join(shortSocketDir(t), "s.sock")
		if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
			t.Fatalf("write regular file: %v", err)
		}

		_, err := listen(path)
		if err == nil {
			t.Fatal("expected an error binding over a regular file")
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("regular file was removed despite listen refusing it: %v", statErr)
		}
	})

	t.Run("binds with mode 0600", func(t *testing.T) {
		path := filepath.Join(shortSocketDir(t), "s.sock")
		ln, err := listen(path)
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer ln.Close()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("socket mode = %o, want 0600", perm)
		}
	})
}

func TestServeRemovesSocketOnShutdown(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "s.sock")
	ln, err := listen(path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	s := New(Options{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected the socket file to be removed, stat err = %v", err)
	}
}

// TestSocketTransport exercises the real listen/Serve path end to end
// over an actual unix socket, not just http.Handler.ServeHTTP.
func TestSocketTransport(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "s.sock")
	ln, err := listen(path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	s := New(Options{Config: &fakeStore{cfg: sampleConfig()}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Serve(ctx, ln)

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}}

	var resp *http.Response
	waitForListener(t, func() (err error) {
		resp, err = client.Get("http://knobd/config")
		return err
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /config over the real socket: status = %d, want 200", resp.StatusCode)
	}
}

func waitForListener(t *testing.T, try func() error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if lastErr = try(); lastErr == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("listener never became reachable: %v", lastErr)
}
