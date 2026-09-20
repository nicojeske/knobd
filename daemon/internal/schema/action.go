package schema

import (
	"fmt"
	"reflect"

	"github.com/invopop/jsonschema"

	"github.com/njeske/knobd/internal/model"
)

// actionSchemas builds one oneOf branch per model.ActionTypes() entry —
//
//	{"type": {"const": "volume.adjust"}, "params": {"$ref": "#/$defs/VolumeAdjustAction"}}
//
// — plus every $defs entry those $refs need (each action's params
// struct, and anything it in turn references, e.g. model.Target). r's
// Mapper must already be set: a params struct can itself contain one of
// the enum types (e.g. MediaTransportAction.Command) that needs the same
// treatment schema.Generate gives everything else.
//
// r must not have ExpandedStruct set — actionSchemas needs each action
// type to resolve to a real $ref plus its own $defs entry, not an
// inlined copy.
func actionSchemas(r *jsonschema.Reflector) (oneOf []*jsonschema.Schema, defs jsonschema.Definitions) {
	defs = jsonschema.Definitions{}
	for _, at := range model.ActionTypes() {
		zero, ok := model.ZeroAction(at)
		if !ok {
			// Unreachable: at came from model.ActionTypes(), which is
			// keyed off the same actionRegistry ZeroAction reads.
			panic(fmt.Sprintf("schema: model.ZeroAction(%q) missing despite ActionTypes() listing it", at))
		}

		nested := r.ReflectFromType(reflect.TypeOf(zero))
		for name, def := range nested.Definitions {
			defs[name] = def
		}

		branch := &jsonschema.Schema{
			Type:                 "object",
			Properties:           jsonschema.NewProperties(),
			Required:             []string{"type", "params"},
			AdditionalProperties: jsonschema.FalseSchema,
		}
		branch.Properties.Set("type", &jsonschema.Schema{Const: string(at)})
		branch.Properties.Set("params", &jsonschema.Schema{Ref: nested.Ref})
		oneOf = append(oneOf, branch)
	}
	return oneOf, defs
}
