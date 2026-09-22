// Command schemagen regenerates knobd's generated documentation from
// daemon/internal/model and daemon/internal/api, via daemon/internal/schema:
// docs/config.schema.json (JSON Schema), docs/openapi.json (OpenAPI 3.1),
// and docs/device-layout.json (hardware index ranges and gesture matrix).
// It is a separate binary from cmd/knobd, not a knobd flag, so that
// github.com/invopop/jsonschema and its transitive dependencies never
// link into the daemon that actually ships (see
// specs/adr/0005-schema-generation-via-invopop.md).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/njeske/knobd/internal/schema"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
}

func run() error {
	kind := flag.String("kind", "config", "which document to generate: config, openapi, or device-layout")
	out := flag.String("o", "docs/config.schema.json", "path to write the generated document to")
	flag.Parse()

	var (
		data []byte
		err  error
	)
	switch *kind {
	case "config":
		data, err = schema.Generate()
	case "openapi":
		data, err = schema.GenerateOpenAPI()
	case "device-layout":
		data, err = schema.GenerateDeviceLayout()
	default:
		return fmt.Errorf("unknown -kind %q (want config, openapi, or device-layout)", *kind)
	}
	if err != nil {
		return fmt.Errorf("generate %s: %w", *kind, err)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}
	return nil
}
