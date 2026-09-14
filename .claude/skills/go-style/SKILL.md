---
name: go-style
description: Idiomatic Go style distilled from Effective Go, the Go Code Review Comments wiki, and the official module layout doc. Use when writing, reviewing, or restructuring Go code in this repo — especially for naming, error handling, interfaces, doc comments, concurrency patterns, and package layout decisions that linters don't catch.
---

# Go style (Effective Go + Code Review Comments + module layout)

Sources (fetched 2026-07-10):
- https://go.dev/doc/effective_go — canonical but frozen at ~Go 1.0; it
  predates modules and generics, so don't cite it for those.
- https://go.dev/wiki/CodeReviewComments — the de-facto review checklist.
- https://go.dev/doc/modules/layout — official layout recommendations.

This repo runs golangci-lint (gofumpt, unconvert, unparam, wastedassign,
staticcheck…), so formatting and mechanical issues are covered. This skill
is for the judgment calls linters can't make.

## Naming

- `MixedCaps`/`mixedCaps`, never underscores. Unexported constants are
  `maxLength`, not `MAX_LENGTH`.
- Initialisms keep consistent case: `appID`, `ServeHTTP`, `URLPath` —
  never `appId`, `ServeHttp`, `Url`.
- No `Get` prefix on getters: `obj.Owner()` / `obj.SetOwner(x)`.
- Package names: short, lowercase, single word; don't stutter with the
  contents (`bufio.Reader`, not `bufio.BufReader`).
- Variable names scale with distance from declaration: `i`, `r`, `c` for
  tight scopes; descriptive names only for wide/global scope.
- Receivers: one or two letters derived from the type (`c` for `Client`),
  the same letters on every method; never `this`/`self`/`me`.
- One-method interfaces: method + `-er` (`Reader`, `Formatter`). Don't
  reuse canonical names (`Read`, `Write`, `Close`, `String`) with
  different semantics.

## Errors

- Error strings: lowercase, no trailing punctuation
  (`fmt.Errorf("something bad")`) — they get wrapped mid-sentence.
- Never discard errors with `_`; handle, return, or (rarely, truly
  unrecoverable) panic. Libraries return errors, they don't panic.
- Omit `else` after a body ending in `return`/`continue`/`break` — keep
  the happy path at minimum indentation.
- Wrap with context when returning upward: `fmt.Errorf("open config: %w", err)`.

## Interfaces & API design

- **Interfaces live in the consuming package**, not next to the
  implementation. Return concrete types; let consumers define the
  interface they need (and mock it themselves).
- Don't define an interface before something actually uses it, and don't
  create one "for mocking" on the implementor's side.
- Design types so the zero value is useful (`bytes.Buffer`, `sync.Mutex`).

## Receivers: pointer vs value

Pointer when: the method mutates, the struct contains a `sync.Mutex`, or
the struct is large. Value when: map/func/chan, small immutable structs
(`time.Time`-like), basic types. **Never mix within a type; when in
doubt, pointer.**

## Concurrency

- "Don't communicate by sharing memory; share memory by communicating."
- Prefer synchronous functions — return results directly; let the caller
  add `go` if they want concurrency. Keeps goroutine lifetimes local and
  testable. (ai-cli follows this: goroutines talk to the UI only via
  channels → Bubble Tea messages, never touch the model.)
- `context.Context` is always the first parameter, never a struct field:
  `func F(ctx context.Context, …)`.
- Buffered channel as semaphore; `select` with `default` for non-blocking
  ops; a send/receive on an unbuffered channel as a completion signal.

## Doc comments & misc

- Every exported top-level name gets a doc comment: a full sentence
  starting with the name — `// Owner returns …`.
- Package comment sits immediately above `package x` (no blank line);
  for `package main` start with "Binary/Command/Program …".
- Prefer `var t []string` (nil slice) over `t := []string{}` — except
  when the JSON null-vs-`[]` distinction matters.
- Naked returns only in very short functions; named results are for
  documentation or deferred-closure access, not for saving a line.
- Test failures read `Foo(%q) = %d; want %d` (got before want); use
  table-driven tests.
- `defer` for cleanup right after acquiring the resource; args are
  evaluated at defer time, LIFO on return.
- Compile-time interface check: `var _ json.Marshaler = (*T)(nil)`.

## Module layout (go.dev/doc/modules/layout)

- **Basic command** (what ai-cli is): `go.mod` + `package main` files at
  the repo root. Correct as-is — no need for `cmd/` or `pkg/`.
- Growing? Split shared logic into `internal/<pkg>/` — `internal/` blocks
  outside imports, so refactoring stays free. Prefer `internal/` over
  exported packages unless something is deliberately public API.
- Multiple binaries → `cmd/prog1/main.go`, `cmd/prog2/main.go`, shared
  code in `internal/`.
- Never create `src/` or `pkg/` directories — not Go conventions.
