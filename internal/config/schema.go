// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

import (
	_ "embed"
)

// SchemaJSON is the JSON Schema for aibench.yaml — the machine-readable
// twin of the strict parser. `aibench schema` prints it; editors fetch
// it from the artifacts repo via SchemaURL (the modeline every scaffold
// writes), so the app never has to maintain schema copies on disk.
// Anyone offline can pin a local copy by hand:
// `aibench schema > aibench.schema.json` and point the modeline there.
//
//go:embed schema.json
var SchemaJSON []byte

// SchemaURL serves the published schema straight out of this repo's
// schema/ directory, raw off evergreen `main` (`make schema` regenerates
// that copy from the embedded one). The app never checks the modeline: it
// is editor-only lint with no runtime effect — the strict parser is the
// one contract, and it names its own errors. The URL's ref segment is a
// git ref, so anyone wanting a frozen editor schema swaps main for an
// artifacts tag or SHA by hand; SchemaStore registration would retire
// the modeline entirely.
const SchemaURL = "https://raw.githubusercontent.com/petrsx/aibench/main/schema/aibench.schema.json"
