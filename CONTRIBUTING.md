# Contributing

This is the page for working on aibench: building it, testing it, and
finding your way around the code. If you want to know what the app does
and how to use it, start with the [README](README.md).

The first thing to know is that **the code explains itself**. Every
package opens with a comment saying what it is for, and every file with
one saying what it owns. Read those before changing anything inside —
they sit right beside the code and get reviewed with it, so unlike a
wiki they can't quietly go stale. Two places hold what no single
package can: [`CLAUDE.md`](CLAUDE.md) has the rules the whole codebase
follows and an index of the packages (it was written for coding agents,
but the rules bind humans just the same), and
[`docs/development/`](docs/development/) has the notes on things like
gateway quirks and terminal behaviour that touch several packages at
once.

## Build

```sh
make          # build into bin/aibench and run the TUI
make build    # build only
make vet      # go vet
make lint     # golangci-lint
make fmt      # gofmt + golangci-lint fmt
make tidy     # go mod tidy
```

Reach for the Makefile rather than calling `go` or `golangci-lint`
yourself. If you need something it doesn't do yet, add a target — then
the next person gets it for free.

### `AIBENCH_DEV=1`

A switch for things that only make sense while you are building aibench,
not using it. So far there is one: with it set, **pressing `esc` twice
within a second quits the app**, and the help line says so while the
window is open. Without it, `esc` only closes whatever is open and you
leave with `ctrl+c` or by typing `exit` — the way a user would.

It is read once when the app starts, so a session keeps one behaviour
from start to end. A value that isn't a boolean is ignored, with
a note in the app log. It is deliberately not called `AIBENCH_DEBUG`:
`--debug` already means "write a detailed log", and a single word
meaning two things is exactly the kind of ambiguity this tool exists to
remove.

If you add more developer-only behaviour, put it behind this same
switch. A build is either a working copy or it isn't.

## Testing

There are three tiers, and only the first one runs on its own.

**`make test`** is everything that needs no network and no credentials:
the unit tests, plus the TUI end-to-end tier in `internal/tui/e2e`. That
tier drives the real app in-process — scripted key flows, teatest
program runs, sends against an httptest mock — and compares screens to
golden files. When a golden legitimately changes, regenerate it with
`go test ./internal/tui/e2e -update` and read the diff before you commit
it. New TUI behaviour gets a test here. CI runs all of this with `-race`.

**`make tui-shot`** is for looking, not asserting. It launches the app in
a real terminal, sends it keys, and screenshots the result, so you can
eyeball a state you are working on. Knobs are environment variables:
`COLS`/`ROWS`, `KEYS`, `WAIT_TEXT`, `CONFIG_FILE`, `PROMPT_FILE`. It runs
against a throwaway config and never reaches a live gateway.

**`make acc-test`** is the one that talks to the real endpoints: a single
`aibench send` through each profile, with your actual credentials, to
confirm nothing drifted. Run it yourself when you want to know, or
trigger the `acc-test` workflow by hand — it takes its credentials from
the `ACC_BUNDLE` repository secret, and the one-liner that creates that
secret is in the workflow's header. It is never part of the merge gate:
a red live run means an endpoint changed, not that your change is bad.
Knobs: `PROFILES`, `MSG`, `TIMEOUT`.

## Where things live

| Package | What it is |
|---|---|
| `internal/app` | the composition root: wires config, watchers and the TUI together |
| `internal/cli` | the cobra commands (`send`, `profiles`, `pricing`; a hidden `schema`) — none of them need the TUI |
| `internal/tui` + `chat`, `inspector`, `prompt` | the shell and its three tabs; `screen`, `find`, `render`, `selection`, `theme`, `notify` are the shared widgets |
| `internal/state` / `internal/store` | the live request lifecycle / the session log it writes into (turns and records) |
| `internal/provider` | one file per api — chat, responses (agents included), messages — plus the capturing transport |
| `internal/config` / `internal/prefs` | the strict yaml parser, credentials contract, embedded schema, first-run scaffold / settings.json, the app's own file |
| `internal/preset` / `internal/bindings` | the prompt and starter files / the tool files and what executes them |
| `internal/capture` / `pricing` / `applog` / `update` / `version` | captured HTTP data, model rates, the app log, the release check, the build version |

## Things that will bite you

- The TUI is Bubble Tea **v2**, imported as
  `charm.land/{bubbletea,bubbles,lipgloss}/v2`. Don't bring back a
  `github.com/charmbracelet/*` v1 import, however familiar it looks.
- Everything happens inside the message loop. A goroutine never touches
  the model; it sends a message and the loop does the work.
- Keybindings use modifiers, because a plain letter is something the user
  is probably typing into the chat.
- Config parsing is strict and fails loudly. An unknown key or an invalid
  combination of knobs is a named error, never a silent drop. And since
  this is pre-release, an old spelling gets deleted rather than aliased.
- `examples/` stays generic. Your own profiles, prompts and notes live
  untracked in `.aibench/`, never in the repo.

## Licensing contributions

aibench is [MIT](LICENSE). By opening a pull request you agree your
contribution is licensed the same way, so the whole tree stays under one
license with no per-file exceptions.

## Releasing

Three workflows carry a change from a push to a package:

- **`ci.yml`** runs on every push to main and every pull request: build,
  `go test -race` on each of the three operating systems we ship for, and
  golangci-lint.
- **`cross-build.yml`** runs the release build for real — every goreleaser
  target — but publishes nothing. Its job is to notice that a platform
  stopped compiling before a tag exists. The binaries stay attached as an
  artifact if you want to try one.
- **`release.yml`** fires when you publish a GitHub release. goreleaser
  builds the first-class Go platforms, stamps the version into the binary,
  writes a binary formula into the Homebrew tap, and pushes the winget
  manifests to our fork and opens the pull request against
  microsoft/winget-pkgs (on the `WINGET_TOKEN` classic PAT, `public_repo`
  scope — the app token has no standing on Microsoft's repo).

### Before a release

- **Refresh the published pricing table** — `rm pricing/pricing.yaml`
  then `aibench pricing update --out pricing/pricing.yaml`, so a fresh
  install starts on current rates.

## Roadmap

- [ ] **Tool-result replay setting.** A `/settings` row for the first
  request-shaping choice: replay past tool results verbatim (today), or
  elide stale ones to a stub so a long conversation stops growing into the
  gateway's size limit. A starter script that drives a tool loop replays
  the same conversation either way, so the two settings can be compared
  request by request in the Inspector.
- [ ] **`aibench tools import <spec>` — OpenAPI → tools file.** Per
  operation: the declaration in each wire's shape, plus an `http` binding
  from the params the spec declares. Offline and reviewable as a diff,
  never a runtime translator — the file on disk must stay exactly what goes
  on the wire. Has to fail loudly and report what it skipped; OpenAPI is
  bigger than a first cut can cover. See
  [tool-catalog](docs/tool-catalog.md#not-supported).
