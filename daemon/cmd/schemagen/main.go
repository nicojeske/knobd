// Command schemagen regenerates docs/config.schema.json from
// daemon/internal/model, via daemon/internal/schema. It is a separate
// binary from cmd/knobd, not a knobd flag, so that
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
	out := flag.String("o", "docs/config.schema.json", "path to write the generated JSON Schema to")
	flag.Parse()

	data, err := schema.Generate()
	if err != nil {
		return fmt.Errorf("generate schema: %w", err)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}
	return nil
}
