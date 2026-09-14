# CLAUDE.md

## What this is

A TUI for prompt engineering and testing AI endpoints — a developer tool, not a chat client. Three tabs: Chat (conversation), Inspector (raw request/response per record), Prompt (rendered instructions + params). Bubble Tea **v2** — the charm.land stack (`charm.land/{bubbletea,bubbles,lipgloss}/v2`); never reintroduce `github.com/charmbracelet/*` v1 imports.

**A package documents itself.** Open its `.go` files and read the package and file comments before working inside it; they sit beside the code and cannot drift. This file is the always-loaded summary: commands, rules, a package index.

## Commands

- `make` — build into `bin/aibench` and run. `make build` / `vet` / `lint` / `fmt` / `tidy` / `test`. Prefer Makefile targets over raw `go`/`golangci-lint`; add a target rather than hand-typing a command.
- `make acc-test` — one real send per profile against **LIVE endpoints**. Developer-run ONLY — never run or trigger it yourself. In CI it is a manual workflow, never part of the merge gate: a red live run means endpoint drift, not a bad change.
- `make tui-shot` — screenshot the real app to *eyeball* a state. Knobs: `COLS`/`ROWS`, `WAIT_TEXT`, `KEYS`, `CONFIG_FILE`, `PROMPT_FILE`, `PRICING_FILE`. Runs on a generated config, never the developer's, never a live gateway. **Never hand-roll `shell-use` chains** — extend the script for one-offs.
- `make tui-gif` — *record* a flow (`TAPE=` picks a tape from `scripts/vhs/`, one per scripted operation; needs `brew install vhs`; gifs land in `bin/`). Same isolation as tui-shot.
- To *assert* behavior: Go tests in `internal/tui/e2e` — pumped flows over the shell's public surface, teatest program flows, httptest mocks, golden frames in `e2e/testdata`. Regenerate goldens deliberately with `go test ./internal/tui/e2e -update` and review the diff. New TUI behavior gets a test there.
- The CLI (`internal/cli`) works without the TUI: `aibench` (the app), `aibench send` (one request, or `--script` a starter), `aibench profiles edit`, `aibench pricing update|edit`. `schema` and `profiles list` exist hidden for `make schema` and acc-test.

## Configuration

`aibench.yaml` is the human's file and the app never writes it. Three axes: **`profiles:`** (endpoint — kind, api, auth; may pin a set via `prompt:`), **`prompts:`** (named sets — `instructions:` + `starters:` + `tools:` key references), **`tools:`** (named tool files). Parsed strictly: an unknown key is an error. The file is required — no file anywhere is a first run that scaffolds one; a pinned path (`--config`, `CONFIG_FILE`) that is missing is an error. What the app persists lives in `settings.json` beside it (`internal/prefs`). Credentials live only in the `.env.*` files a profile names — **never commit or print them**. `examples/` ships generic copy-ready content; the developer's own profiles and prompts live untracked in `.aibench/`. The schema (`internal/config/schema.json`) is embedded and published raw from this repo's `schema/`, as `pricing/` is; `make schema` regenerates the copy. User docs: `docs/configuration.md`, `docs/prompts.md`, `docs/authentication.md`.

## Load-bearing rules

- **A profile resolves loudly.** `kind:` is required, never defaulted: `model` | `agent` (agent implies the responses wire + server state and rejects foreign knobs). Three wires: `responses` (openai, target for new work; alone carries `store`/`stream`), `chat` (interoperable default), `messages` (anthropic). The route is entirely `api.base`'s business (may carry `{model}`; `api-version` rides `query:`) — no host axis. Auth is declared, never inferred: exactly one credential method per profile, a second is a named error; only `api.headers` composes on top; the token audience derives from the api. An invalid combination always fails with a name, never drops silently.
- **Api knowledge lives in `internal/provider` only.** `SpecFor` and `NewClient` are the doors; the api is the unit, each owns its whole wire. The TUI hardcodes nothing api-specific — a request body's shape is `Spec.Body`, and every captured record carries the api that produced it (`capture.Event.API`) so an old body is read by its own rules.
- **Pre-release: delete, don't alias.** No backward compatibility — a legacy spelling is removed, a replaced feature is ripped out cleanly, no migration sweeps.
- **A prompt set belongs to its profile.** A profile is where requests go *and* what is sent with them: `prompt:` picks the set, an unpinned profile gets the first set by name, and switching profile switches both and clears the conversation (re-resolving the same profile on hot-reload keeps it). There is no `/prompt`: two ways to choose one thing. Starters are scripted conversations; declaring them makes launch *offer* `/starters` — **nothing ever auto-sends**.
- **Every human file hot-reloads.** `aibench.yaml`, the prompt and starter files, and `pricing.yaml` are watched by `internal/fswatch`; the watcher only signals, the owner re-reads. Nothing needs a restart.
- **`store` + `state` are the shared truth; views pull.** `internal/store` is the session log; `internal/state` is the live layer over it (Send → Receive → Capture, plus `Progress`). Tabs hold `*state.State` and derive every view per render, memoized on `store.Version()`; the shell pushes nothing. One request/response is a **record**. The CLI's `send` drives the same store and state through a headless message pump — there is one send path.
- **Tabs are independent `tea.Model`s; the shell composes and routes.** No view logic and no request lifecycle in the shell. Record selection (pin) lives in the store.
- **`model.go` is a package's readable spine** — struct, `New`, `Init`, `Update`, thin `View`. Every other concern lives in a file named for the piece it owns; new behavior goes into its piece's file, never accreted onto `model.go`.
- **Single-threaded by message loop.** Goroutines never touch the model; `store` and `state` mutate only inside the loop — no locks.
- **Actions are slash commands; chords are minimal.** The registry in `tui/commands.go` is the one action surface: the session group first (`/profiles`, `/starters`, `/clear`), then the rest a→z. Picking commands use the inline selector (the Claude Code /model shape — no modal pickers). Chords are only what a command cannot cover (ctrl+c/esc, shift+tab, ctrl+f, per-pane editing keys), modifier-based because plain letters collide with typing.
- **Exactly one HTTP request per model call** — SDK retries are disabled. A send may span several calls through the bound-tool loop (capped at 10 rounds), each call its own record.
- **Tools are a config axis, merged verbatim.** Prompt sets reference `tools:` keys (list order = merge order; agents may not). Overlays merge into the request byte-for-byte in each api's wire shape: across files `tools` arrays concatenate, any other collision is a named error. aibench-only `bindings` (http | static | mcp) are stripped before sending; a tool without a binding is inspect-only; a declared file that is missing is an error.
- **Conversations are not persisted.** Repeatable conversations belong in starter scripts. `/clear` resets; a profile switch clears too.

## Package index

One line each, purpose only — the package's own comments carry the files and the detail.

- **`internal/tui`** — the shell: composes the tabs, routes messages, owns the slash registry, profiles and prompts, the settings rows, the editor hand-off.
- **`internal/tui/chat`** / **`inspector`** / **`prompt`** — the three tabs, each an independent model over the shared state.
- **`internal/tui/screen`** — the /settings + /help screen; a leaf widget, the shell owns meaning.
- **`internal/tui/theme`** / **`render`** / **`selection`** / **`find`** / **`notify`** — shared leaves: adaptive styles, pure presentation helpers, mouse text selection, the find widget, the transient notice (a tab's own for its pane, the shell's `notice.go` for app-level messages).
- **`internal/state`** — the live send lifecycle over the store, tool loop included.
- **`internal/store`** — the session log: turns and records, stable IDs, bounded, versioned; the pin lives here.
- **`internal/provider`** — provider-neutral types, the capturing transport (secrets redacted), one file per api owning its whole wire. Request-shape tests pin routes, headers and bodies.
- **`internal/capture`** — the captured HTTP data model; pure leaf.
- **`internal/bindings`** — tool files: merging with fail-loud collisions, verbatim overlays, the http/static/mcp executors (`Run` only inside a `tea.Cmd`).
- **`internal/preset`** — the prompt and starter files.
- **`internal/fswatch`** — the one directory watcher behind every hot-reload.
- **`internal/config`** — aibench.yaml: strict parsing, profile resolution, the credentials contract, prompt sets, the embedded schema, the first-run scaffold.
- **`internal/prefs`** — settings.json: what the app remembers, the active-profile bookkeeping, the editor resolver.
- **`internal/cli`** — the cobra surface; `errors.go` is the one pre-TUI failure format.
- **`internal/app`** — composition: `Run()` wires config, watchers and the TUI, or scaffolds on a first run.
- **`internal/pricing`** — model rates for the Metrics cost rows: the local file, else the published table fetched once at launch.
- **`internal/applog`** / **`update`** / **`version`** — the app log (never secrets), the quiet release check, the build version.

## Gotchas

- One file per subject in [`docs/development/`](docs/development/) — notes for whoever works on the code. [apim](docs/development/apim.md): gateway routes, `api-version`, what a generic 500 means — read it before touching provider or request code. [tui](docs/development/tui.md): terminal glyph coverage, and why `View.OnMouse` is unused.
- User docs (`docs/`, README, CONTRIBUTING) are written for a person: say why you would reach for something, then what happens. Never spec-style "resolves / declared / honored verbatim" prose.
