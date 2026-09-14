// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Command aibench is a TUI for testing an AI endpoint (OpenAI/Anthropic,
// direct or on Azure Foundry), with live diagnostics to understand the API
// behaviour. Running it with no arguments launches the TUI; subcommands
// (e.g. `aibench pricing update`) do maintenance. All wiring lives in
// internal/cli.
package main

import "github.com/petrsx/aibench/internal/cli"

func main() { cli.Execute() }
