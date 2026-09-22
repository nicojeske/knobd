package schema

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/njeske/knobd/internal/api"
)

// TestGenerateOpenAPIDeterministic mirrors TestGenerateDeterministic:
// the same route table and model must always produce byte-identical
// output, which is what makes gating CI on a committed
// docs/openapi.json meaningful.
func TestGenerateOpenAPIDeterministic(t *testing.T) {
	a, err := GenerateOpenAPI()
	if err != nil {
		t.Fatalf("GenerateOpenAPI: %v", err)
	}
	b, err := GenerateOpenAPI()
	if err != nil {
		t.Fatalf("GenerateOpenAPI: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("GenerateOpenAPI() produced different output across two calls in the same process")
	}
}

// TestGenerateOpenAPIMatchesCommitted is the drift gate: CI runs `make
// schema` and diffs docs/openapi.json, but this test catches the same
// drift locally, without a build step.
func TestGenerateOpenAPIMatchesCommitted(t *testing.T) {
	got, err := GenerateOpenAPI()
	if err != nil {
		t.Fatalf("GenerateOpenAPI: %v", err)
	}
	want, err := os.ReadFile("../../../docs/openapi.json")
	if err != nil {
		t.Fatalf("read docs/openapi.json (run `make schema` from the repo root after an api/model change): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Error("docs/openapi.json is out of date; run `make schema` from the repo root to regenerate it")
	}
}

// TestOpenAPIRoutesComplete checks every api.Routes() entry produced a
// path+operation, and every RequestBody/response Schema name it named
// resolves to a real component schema.
func TestOpenAPIRoutesComplete(t *testing.T) {
	doc := generatedOpenAPIDoc(t)
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal(`"paths" missing or not an object`)
	}
	schemas, ok := doc["components"].(map[string]any)["schemas"].(map[string]any)
	if !ok {
		t.Fatal(`"components.schemas" missing or not an object`)
	}

	for _, route := range api.Routes() {
		item, ok := paths[route.Path].(map[string]any)
		if !ok {
			t.Errorf("path %q missing from the generated document", route.Path)
			continue
		}
		op, ok := item[httpMethodKey(route.Method)].(map[string]any)
		if !ok {
			t.Errorf("%s %s: no operation found for method key %q", route.Method, route.Path, httpMethodKey(route.Method))
			continue
		}
		if got := op["operationId"]; got != route.OperationID {
			t.Errorf("%s %s: operationId = %v, want %q", route.Method, route.Path, got, route.OperationID)
		}
		if route.RequestBody != "" {
			if _, ok := schemas[route.RequestBody]; !ok {
				t.Errorf("%s %s: requestBody schema %q not in components.schemas", route.Method, route.Path, route.RequestBody)
			}
		}
		for _, resp := range route.Responses {
			if resp.Schema == "" {
				continue
			}
			if _, ok := schemas[resp.Schema]; !ok {
				t.Errorf("%s %s: response %d schema %q not in components.schemas", route.Method, route.Path, resp.Status, resp.Schema)
			}
		}
	}
}

// TestOpenAPINoUnrewrittenRefs guards the $defs -> components/schemas
// rewrite: a survived "#/$defs/" reference would be a broken document
// openapi-typescript can't resolve.
func TestOpenAPINoUnrewrittenRefs(t *testing.T) {
	data, err := GenerateOpenAPI()
	if err != nil {
		t.Fatalf("GenerateOpenAPI: %v", err)
	}
	if bytes.Contains(data, []byte(`$defs`)) {
		t.Error("generated OpenAPI document still contains a $defs reference")
	}
}

// TestOpenAPIComponentTypesComplete checks componentTypes covers Config
// and ErrorResponse (the two component schemas today's route table
// references directly) -- the same structural-coverage spirit as
// schema_test.go's forgotten-field check, applied to the set of
// top-level component types rather than one type's fields.
func TestOpenAPIComponentTypesComplete(t *testing.T) {
	for _, route := range api.Routes() {
		if route.RequestBody != "" {
			assertComponentTypeDeclared(t, route.RequestBody)
		}
		for _, resp := range route.Responses {
			if resp.Schema != "" {
				assertComponentTypeDeclared(t, resp.Schema)
			}
		}
	}
}

func assertComponentTypeDeclared(t *testing.T, name string) {
	t.Helper()
	// ErrorResponse, Config, and State are reflected root types
	// (componentTypes); everything else (Action, AppMatcher, ...) is
	// reached transitively and only needs to exist in the generated
	// document, which TestOpenAPIRoutesComplete already checks.
	switch name {
	case "Config", "State", "AudioGraph", "Capabilities", "ErrorResponse":
		if _, ok := componentTypes[name]; !ok {
			t.Errorf("route references component schema %q, which is not in componentTypes", name)
		}
	}
}

func generatedOpenAPIDoc(t *testing.T) map[string]any {
	t.Helper()
	data, err := GenerateOpenAPI()
	if err != nil {
		t.Fatalf("GenerateOpenAPI: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal generated OpenAPI document: %v", err)
	}
	return doc
}
