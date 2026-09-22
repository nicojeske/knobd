package api

import "testing"

// TestRoutesHaveHandlers gates the two things routes.go and server.go
// must never let drift apart: every declared Route has a registered
// handler, and every registered handler corresponds to a declared
// Route. A route landing in one without the other would otherwise only
// surface as registerRoutes's panic at daemon startup, or as an
// undocumented endpoint missing from docs/openapi.json.
func TestRoutesHaveHandlers(t *testing.T) {
	s := New(Options{})
	handlers := s.handlers()
	routes := Routes()

	routeIDs := make(map[string]bool, len(routes))
	for _, r := range routes {
		routeIDs[r.OperationID] = true
		if _, ok := handlers[r.OperationID]; !ok {
			t.Errorf("route %s %s (operationId %q) has no registered handler", r.Method, r.Path, r.OperationID)
		}
	}
	for id := range handlers {
		if !routeIDs[id] {
			t.Errorf("handler registered for operationId %q, which is not in Routes()", id)
		}
	}
}

// TestRoutesUnique catches a copy-pasted Route whose Method+Path was
// never updated -- net/http.ServeMux would silently let the second
// registration win.
func TestRoutesUnique(t *testing.T) {
	seen := make(map[string]string)
	for _, r := range Routes() {
		key := r.Method + " " + r.Path
		if prev, ok := seen[key]; ok {
			t.Errorf("route %s registered twice: operationIds %q and %q", key, prev, r.OperationID)
		}
		seen[key] = r.OperationID
	}
}

// TestRegisterRoutesDoesNotPanic exercises New (which calls
// registerRoutes) as a smoke test for the handlers()/Routes() mapping
// actually used at daemon startup.
func TestRegisterRoutesDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New panicked: %v", r)
		}
	}()
	New(Options{})
}
