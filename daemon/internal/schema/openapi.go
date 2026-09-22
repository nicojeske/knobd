package schema

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"

	"github.com/njeske/knobd/internal/api"
)

// componentTypes maps every OpenAPI component schema name a
// api.Routes() entry can reference (RequestBody, or a RouteResponse's
// Schema) to the Go type it is reflected from. openapi_test.go asserts
// these two sets match exactly -- the same structural-coverage spirit
// as schema_test.go's forgotten-field check, applied to the API surface
// instead of the config schema.
var componentTypes = map[string]any{
	"Config":        Config{},
	"State":         api.State{},
	"AudioGraph":    api.AudioGraph{},
	"Capabilities":  api.Capabilities{},
	"ErrorResponse": api.ErrorResponse{},
}

// document/info/pathItem/etc. are a small local OpenAPI 3.1 document
// model -- just enough of the spec for this API's own surface, plain
// encoding/json structs rather than a third-party library. Adding one
// (kin-openapi, libopenapi) would pull a dependency into schemagen for
// something this small; see specs/adr/0005-schema-generation-via-invopop.md
// for the same reasoning applied to jsonschema itself.
type document struct {
	OpenAPI    string              `json:"openapi"`
	Info       info                `json:"info"`
	Paths      map[string]pathItem `json:"paths"`
	Components components          `json:"components"`
}

type info struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

type pathItem map[string]operation // keyed by lowercase HTTP method

type operation struct {
	OperationID string              `json:"operationId"`
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	RequestBody *requestBody        `json:"requestBody,omitempty"`
	Responses   map[string]response `json:"responses"`
}

type requestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]mediaType `json:"content"`
}

type response struct {
	Description string               `json:"description"`
	Content     map[string]mediaType `json:"content,omitempty"`
}

type mediaType struct {
	Schema json.RawMessage `json:"schema"`
}

type components struct {
	Schemas map[string]json.RawMessage `json:"schemas"`
}

// refSchema is a tiny helper for a {"$ref": "..."} mediaType/schema body.
func refSchema(name string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"$ref":"#/components/schemas/%s"}`, name))
}

// GenerateOpenAPI returns an OpenAPI 3.1 document describing api.Routes()
// as indented JSON with a trailing newline, matching Generate()'s style.
// Deterministic, like Generate() -- see openapi_test.go.
func GenerateOpenAPI() ([]byte, error) {
	schemas, err := componentSchemas()
	if err != nil {
		return nil, fmt.Errorf("schema: openapi: component schemas: %w", err)
	}

	doc := document{
		OpenAPI: "3.1.0",
		Info: info{
			Title: "knobd",
			Description: "knobd's local control API, served over a unix domain socket " +
				"(see specs/adr/0004-ipc-over-unix-socket.md). Not a public/networked API: " +
				"filesystem permissions on the socket are the entire access control mechanism.",
			Version: "0.0.0",
		},
		Paths:      map[string]pathItem{},
		Components: components{Schemas: schemas},
	}

	for _, route := range api.Routes() {
		item, ok := doc.Paths[route.Path]
		if !ok {
			item = pathItem{}
		}
		op := operation{
			OperationID: route.OperationID,
			Summary:     route.Summary,
			Description: route.Description,
			Responses:   map[string]response{},
		}
		if route.RequestBody != "" {
			op.RequestBody = &requestBody{
				Required: true,
				Content:  map[string]mediaType{"application/json": {Schema: refSchema(route.RequestBody)}},
			}
		}
		for _, resp := range route.Responses {
			r := response{Description: resp.Description}
			if resp.Schema != "" {
				contentType := "application/json"
				if route.OperationID == "events" {
					contentType = "text/event-stream"
				}
				r.Content = map[string]mediaType{contentType: {Schema: refSchema(resp.Schema)}}
			}
			op.Responses[fmt.Sprintf("%d", resp.Status)] = r
		}
		item[httpMethodKey(route.Method)] = op
		doc.Paths[route.Path] = item
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("schema: openapi: marshal: %w", err)
	}
	// invopop emits refs as "#/$defs/X"; OpenAPI's own convention (and
	// what openapi-typescript expects) is "#/components/schemas/X". Both
	// encoding/json's map key ordering and invopop's Definitions
	// insertion order are deterministic, so this is a safe, testable
	// string substitution rather than a schema walk.
	data = bytes.ReplaceAll(data, []byte(`#/$defs/`), []byte(`#/components/schemas/`))
	if bytes.Contains(data, []byte(`#/$defs/`)) {
		return nil, fmt.Errorf("schema: openapi: unrewritten #/$defs/ reference survived")
	}
	return append(data, '\n'), nil
}

func httpMethodKey(method string) string {
	switch method {
	case "GET":
		return "get"
	case "PUT":
		return "put"
	case "POST":
		return "post"
	case "DELETE":
		return "delete"
	case "PATCH":
		return "patch"
	default:
		return method
	}
}

// componentSchemas reflects every entry in componentTypes into an
// OpenAPI components/schemas map. Each type is reflected with its own
// Reflector.Reflect call (not ExpandedStruct), which -- per
// invopop/jsonschema's behavior -- returns a $ref to "#/$defs/<TypeName>"
// plus a Definitions map holding that type and everything it
// transitively references; those maps are merged, so a type referenced
// by more than one component (e.g. model.Target, used by both Config and
// State) is reflected once and shared, exactly like a real $defs would
// be. Config gets the same Action-oneOf treatment Generate() gives it.
func componentSchemas() (map[string]json.RawMessage, error) {
	merged := jsonschema.Definitions{}

	for name, v := range componentTypes {
		r := &jsonschema.Reflector{Anonymous: true, Mapper: mapper}
		root := r.Reflect(v)
		for defName, def := range root.Definitions {
			merged[defName] = def
		}
		if _, ok := merged[name]; !ok {
			return nil, fmt.Errorf("schema: openapi: reflecting %s did not produce a %q definition (got ref %q)", name, name, root.Ref)
		}
	}

	// Config's Action field needs the same oneOf-over-actionRegistry
	// treatment Generate() gives it -- reflection alone can't see
	// through the model.Action interface.
	oneOf, actionDefs := actionSchemas(&jsonschema.Reflector{Anonymous: true, Mapper: mapper})
	for name, def := range actionDefs {
		merged[name] = def
	}
	merged["Action"] = &jsonschema.Schema{OneOf: oneOf}

	// encoding/json sorts map keys alphabetically on marshal, so the
	// component schema order in the final document is deterministic
	// without sorting here explicitly.
	out := make(map[string]json.RawMessage, len(merged))
	for name, def := range merged {
		data, err := json.Marshal(def)
		if err != nil {
			return nil, fmt.Errorf("schema: openapi: marshal component %s: %w", name, err)
		}
		out[name] = data
	}
	return out, nil
}
