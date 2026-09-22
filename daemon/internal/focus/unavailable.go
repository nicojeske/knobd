package focus

import "context"

// unavailableProvider is a Provider that reports no focused window,
// ever. It is what knobd runs with until M06 lands focus.New's real
// implementation (see the package doc comment), so TargetFocused and
// knob.assign_focused_app fail with one clear, greppable error
// (ErrUnavailable) instead of engine.Deps holding a nil Provider that
// every call site would have to nil-check.
type unavailableProvider struct{}

// Unavailable returns a Provider that never reports a focused window.
// It is a documented production stand-in — distinct from
// NewFakeProvider, which is for tests — so a system with no window-
// manager integration (or one running before M06) still gets a valid,
// required Deps.Focus.
func Unavailable() Provider {
	return unavailableProvider{}
}

func (unavailableProvider) Watch(ctx context.Context) (<-chan AppInfo, error) {
	ch := make(chan AppInfo)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (unavailableProvider) Current(context.Context) (AppInfo, error) {
	return AppInfo{}, ErrUnavailable
}

func (unavailableProvider) Close() error { return nil }

var _ Provider = unavailableProvider{}
