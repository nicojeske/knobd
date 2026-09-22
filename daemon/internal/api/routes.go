package api

// Route describes one entry in this API's surface: enough to both
// register it on the mux and describe it in docs/openapi.json (see
// daemon/internal/schema/openapi.go), so a route cannot exist in one
// without the other. routes_test.go checks both directions.
type Route struct {
	Method      string
	Path        string
	OperationID string
	Summary     string
	Description string
	// RequestBody names the component schema of the request body, or ""
	// if the route takes none.
	RequestBody string
	Responses   []RouteResponse
}

// RouteResponse is one documented response for a Route.
type RouteResponse struct {
	Status int
	// Schema names the component schema of the response body, or "" for
	// a route response with no body (e.g. 204, 403).
	Schema      string
	Description string
}

// Routes is the single declaration of this API's surface. registerRoutes
// builds the mux from it, and daemon/internal/schema emits
// docs/openapi.json from it -- so the two cannot drift apart. New routes
// are added here alongside their handler, one commit each (see
// specs/milestones/M07-config-ui.md).
func Routes() []Route {
	return []Route{
		{
			Method:      "GET",
			Path:        "/config",
			OperationID: "getConfig",
			Summary:     "Get the running configuration",
			Description: "Returns the configuration the daemon is running right now.",
			Responses: []RouteResponse{
				{Status: 200, Schema: "Config", Description: "The running configuration."},
				{Status: 503, Schema: "ErrorResponse", Description: "No configuration store is wired up."},
			},
		},
		{
			Method:      "PUT",
			Path:        "/config",
			OperationID: "putConfig",
			Summary:     "Replace the running configuration",
			Description: "Validates, applies to the running engine, and durably persists a new configuration. Takes effect immediately; no restart needed.",
			RequestBody: "Config",
			Responses: []RouteResponse{
				{Status: 204, Description: "Applied and persisted."},
				{Status: 400, Schema: "ErrorResponse", Description: "Malformed JSON, an unsupported schemaVersion, or a config that fails validation."},
				{Status: 500, Schema: "ErrorResponse", Description: "Applying or persisting failed."},
				{Status: 503, Schema: "ErrorResponse", Description: "No configuration store is wired up."},
			},
		},
		{
			Method:      "GET",
			Path:        "/state",
			OperationID: "getState",
			Summary:     "Get a point-in-time state snapshot",
			Description: "Device/audio/focus/profile status and every currently-bound control's resolved value. Pollable; see GET /events for a live push feed of the same shape.",
			Responses: []RouteResponse{
				{Status: 200, Schema: "State", Description: "The current snapshot."},
				{Status: 503, Schema: "ErrorResponse", Description: "No state provider is wired up."},
			},
		},
	}
}
