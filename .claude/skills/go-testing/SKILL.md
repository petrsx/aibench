---
name: go-testing
description: Go testing conventions for this repo plus the modern testing APIs (t.Setenv, t.Cleanup, T.Context, B.Loop, testing/synctest, fuzzing) that post-date most training habits. Use when writing or reviewing Go tests here. Companion to [[go-style]] (which covers test failure messages and naming).
---

# Go testing (modern APIs + repo conventions)

Verified against pkg.go.dev/testing and pkg.go.dev/testing/synctest
(fetched 2026-07-10). The basics (table-driven tests, subtests via
`t.Run`, `t.Parallel()`) are assumed known; [[go-style]] covers failure
message format (`Foo(%q) = %d; want %d`, got before want).

## Modern APIs worth reaching for (with Go version)

- `t.Setenv(k, v)` (1.17) — env var scoped to the test, auto-restored.
  Setting `""` behaves as unset for code using the empty-means-default
  pattern. Incompatible with `t.Parallel()`.
- `t.Cleanup(f)` (1.14) — LIFO teardown; prefer over defer in helpers.
- `t.TempDir()` (1.15) — auto-removed directory.
- `t.Chdir(dir)` / `t.Context()` (1.24) — scoped working dir; a context
  canceled just before Cleanup runs.
- `b.Loop()` (1.24) — `for b.Loop() { … }` replaces `for i := 0; i < b.N`;
  setup/teardown outside the loop are automatically untimed.
- `testing/synctest` (stable 1.25): `synctest.Test(t, func(t *testing.T))`
  runs concurrent code in an isolated bubble with a **fake clock**
  (starts 2000-01-01, advances only when all goroutines are durably
  blocked) — `time.Sleep`-heavy tests run instantly and deterministically.
  `synctest.Wait()` blocks until all other bubble goroutines are parked.
- Fuzzing (1.18): `func FuzzX(f *testing.F)` + `f.Add(seed)` +
  `f.Fuzz(func(t *testing.T, in string) { … })`.

## Repo conventions (ai-cli)

- Run via `make test` (never raw `go test`); CI runs `go test -race`.
- Two layers, don't mix them:
  - **Unit tests** (`*_test.go` beside the code): pure logic — config
    parsing, diag transport (fake `http.RoundTripper`), tui history /
    selection math, Update-loop behavior driven by constructed
    `tea.KeyMsg`s. No pty, no network.
  - **E2E smoke** (`scripts/tui-test.sh`, `make tui-test`): real binary in
    a pty via shell-use. Keep it thin; new logic gets a unit test first.
- Never let a test hit the real endpoint/gateway or send chat completions
  — use `httptest.Server` for HTTP-level tests; the e2e/shot harnesses
  isolate config and use a dummy endpoint.
- `internal/tui` tests live in the same package (white-box) so they can
  drive `Update` and inspect model state directly; executing a returned
  `tea.Cmd` and type-asserting the `tea.Msg` (e.g. `tea.QuitMsg`) is the
  way to assert on commands.
- Bubble Tea key events are constructed as
  `tea.KeyMsg{Type: tea.KeyEnter}` / `{Type: tea.KeyEscape}`; for runes use
  `{Type: tea.KeyRunes, Runes: []rune("x")}`.
- No `View()` snapshot tests — they break on every style tweak; layout is
  covered by the e2e smoke tests.
