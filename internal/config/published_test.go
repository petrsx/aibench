// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

import (
	"bytes"
	"os"
	"testing"
)

// TestPublishedSchemaMatchesEmbedded pins the schema's two copies together.
// internal/config/schema.json is what the binary embeds and `aibench schema`
// prints; schema/aibench.schema.json is what SchemaURL serves raw off main
// for editors. A reader that fetches the published copy must get exactly
// what the strict parser enforces, so the two are byte-identical or this
// fails — regenerate with `make schema`.
func TestPublishedSchemaMatchesEmbedded(t *testing.T) {
	published, err := os.ReadFile("../../schema/aibench.schema.json")
	if err != nil {
		t.Fatalf("reading published schema: %v", err)
	}
	if !bytes.Equal(published, SchemaJSON) {
		t.Errorf("schema/aibench.schema.json (%d bytes) differs from the embedded schema (%d bytes); run `make schema`",
			len(published), len(SchemaJSON))
	}
}
