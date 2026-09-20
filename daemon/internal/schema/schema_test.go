package schema

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/njeske/knobd/internal/model"
)

// TestGenerateDeterministic guards the property that makes gating CI on
// a committed docs/config.schema.json meaningful at all: the same model
// must always produce byte-identical output.
func TestGenerateDeterministic(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("Generate() produced different output across two calls in the same process")
	}
}

// TestGenerateMatchesCommittedSchema is the drift gate: CI runs `make
// schema` and diffs docs/config.schema.json, but this test catches the
// same drift locally, without a build step, whenever `go test` runs.
func TestGenerateMatchesCommittedSchema(t *testing.T) {
	got, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want, err := os.ReadFile("../../../docs/config.schema.json")
	if err != nil {
		t.Fatalf("read docs/config.schema.json (run `make schema` from the repo root after a model change): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Error("docs/config.schema.json is out of date; run `make schema` from the repo root to regenerate it")
	}
}

// TestActionOneOfComplete checks the "Action" definition has exactly one
// oneOf branch per model.ActionTypes(), each with a const discriminator
// and a $ref to the right params type.
func TestActionOneOfComplete(t *testing.T) {
	doc := generatedDoc(t)
	defs := doc["$defs"].(map[string]any)

	action, ok := defs["Action"].(map[string]any)
	if !ok {
		t.Fatal(`$defs.Action missing or not an object`)
	}
	oneOf, ok := action["oneOf"].([]any)
	if !ok {
		t.Fatal(`$defs.Action.oneOf missing or not an array`)
	}

	types := model.ActionTypes()
	if len(oneOf) != len(types) {
		t.Fatalf("Action.oneOf has %d branches, model.ActionTypes() has %d", len(oneOf), len(types))
	}

	seen := make(map[string]string) // discriminator -> $ref
	for _, branchAny := range oneOf {
		branch, ok := branchAny.(map[string]any)
		if !ok {
			t.Fatalf("oneOf branch is not an object: %#v", branchAny)
		}
		props, ok := branch["properties"].(map[string]any)
		if !ok {
			t.Fatalf("oneOf branch has no properties: %#v", branch)
		}
		typeSchema, ok := props["type"].(map[string]any)
		if !ok {
			t.Fatalf("oneOf branch's \"type\" property is not an object: %#v", props["type"])
		}
		discriminator, ok := typeSchema["const"].(string)
		if !ok {
			t.Fatalf("oneOf branch's \"type\" property has no const: %#v", typeSchema)
		}
		paramsSchema, ok := props["params"].(map[string]any)
		if !ok {
			t.Fatalf("oneOf branch %q has no \"params\" property", discriminator)
		}
		ref, _ := paramsSchema["$ref"].(string)
		seen[discriminator] = ref
	}

	for _, at := range types {
		zero, ok := model.ZeroAction(at)
		if !ok {
			t.Fatalf("model.ZeroAction(%q) missing", at)
		}
		wantRef := "#/$defs/" + reflect.TypeOf(zero).Name()
		gotRef, ok := seen[string(at)]
		if !ok {
			t.Errorf("Action.oneOf has no branch for %q", at)
			continue
		}
		if gotRef != wantRef {
			t.Errorf("Action.oneOf branch for %q has params $ref %q, want %q", at, gotRef, wantRef)
		}
	}
}

// TestStructuralCoverage guards against a field added to model.Config or
// model.Profile without a matching field added to this package's Config
// or Profile shadow types (see schema.go's doc comment for why those
// shadows exist). Every other type reflected into the schema — Control,
// Target, AppMatcher, AppGroup, Scene, SceneEntry, and each action's
// params struct — is reflected from the real model type directly, so it
// can never drift; they're checked here too, cheaply, in case that ever
// changes.
func TestStructuralCoverage(t *testing.T) {
	doc := generatedDoc(t)
	defs := doc["$defs"].(map[string]any)

	assertFieldsCovered(t, "Config", reflect.TypeOf(model.Config{}), doc)
	assertFieldsCovered(t, "Profile", reflect.TypeOf(model.Profile{}), resolveDef(t, defs, "Profile"))
	assertFieldsCovered(t, "AppMatcher", reflect.TypeOf(model.AppMatcher{}), resolveDef(t, defs, "AppMatcher"))
	assertFieldsCovered(t, "AppGroup", reflect.TypeOf(model.AppGroup{}), resolveDef(t, defs, "AppGroup"))
	assertFieldsCovered(t, "Scene", reflect.TypeOf(model.Scene{}), resolveDef(t, defs, "Scene"))
	assertFieldsCovered(t, "SceneEntry", reflect.TypeOf(model.SceneEntry{}), resolveDef(t, defs, "SceneEntry"))
	assertFieldsCovered(t, "Target", reflect.TypeOf(model.Target{}), resolveDef(t, defs, "Target"))
	assertFieldsCovered(t, "Control", reflect.TypeOf(model.Control{}), resolveDef(t, defs, "Control"))

	// model.Binding's own Go fields carry no json tags at all (see
	// binding.go's custom MarshalJSON/UnmarshalJSON); its actual on-disk
	// shape is the unexported bindingJSON, mirrored here by name since
	// that type can't be reflected on from this package.
	assertPropertiesPresent(t, "Binding", []string{"layer", "control", "gesture", "action"}, resolveDef(t, defs, "Binding"))

	for _, at := range model.ActionTypes() {
		zero, ok := model.ZeroAction(at)
		if !ok {
			t.Fatalf("model.ZeroAction(%q) missing", at)
		}
		typ := reflect.TypeOf(zero)
		assertFieldsCovered(t, typ.Name(), typ, resolveDef(t, defs, typ.Name()))
	}
}

func generatedDoc(t *testing.T) map[string]any {
	t.Helper()
	data, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal generated schema: %v", err)
	}
	return doc
}

func resolveDef(t *testing.T, defs map[string]any, name string) map[string]any {
	t.Helper()
	def, ok := defs[name].(map[string]any)
	if !ok {
		t.Fatalf("$defs.%s missing or not an object", name)
	}
	return def
}

// assertFieldsCovered checks that every json-tagged field of typ has a
// corresponding property in schemaNode.
func assertFieldsCovered(t *testing.T, label string, typ reflect.Type, schemaNode map[string]any) {
	t.Helper()
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		names = append(names, name)
	}
	assertPropertiesPresent(t, label, names, schemaNode)
}

func assertPropertiesPresent(t *testing.T, label string, names []string, schemaNode map[string]any) {
	t.Helper()
	props, ok := schemaNode["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s: schema node has no properties object: %#v", label, schemaNode)
	}
	for _, name := range names {
		if _, ok := props[name]; !ok {
			t.Errorf("%s: field %q has no corresponding schema property", label, name)
		}
	}
}
