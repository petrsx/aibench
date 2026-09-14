#!/usr/bin/env bash
# Print a rendered snapshot of the running TUI (for eyeballing layout
# changes). Driven with shell-use; run via `make tui-shot`.
#
# The sanctioned way to inspect the TUI — never hand-roll shell-use
# chains. Knobs (all env vars):
#   COLS/ROWS      terminal size (default 120x36)
#   WAIT_TEXT      block until this text appears (e.g. WAIT_TEXT="tokens")
#   KEYS           keys to press first (e.g. KEYS="Shift+Tab Ctrl+o")
#   CONFIG_FILE    a profiles yaml to load (default: a generated one-profile
#                  config pointing at a dummy endpoint)
#   PROMPT_FILE    a prompt file the generated config declares (default: none)
#   SVG_OUT        also write a full-color SVG screenshot to this path
set -u
cd "$(dirname "$0")/.."

SESSION=aibench-shot
shell-use close --session "$SESSION" >/dev/null 2>&1
# Isolate from the developer's own aibench.yaml (which would point at the
# live gateway) unless the caller passes their own: generate a one-profile
# config at a dummy endpoint, with a prompt set only when PROMPT_FILE says.
BASE="${OPENAI_API_BASE:-http://127.0.0.1:9}"
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
if [ -z "${CONFIG_FILE:-}" ]; then
  CONFIG_FILE="$STAGE/aibench.yaml"
  {
    printf 'profiles:\n  shot:\n    kind: model\n    api:\n      provider: openai\n      base: %s\n      model: shot-model\n    auth:\n      credentials: env\n' "$BASE"
    if [ -n "${PROMPT_FILE:-}" ]; then
      printf 'prompts:\n  shot:\n    instructions: %s\n' "$PROMPT_FILE"
    fi
  } > "$CONFIG_FILE"
fi
CFG="$CONFIG_FILE"
# Isolate from the developer's real pricing.yaml too (the shell fetches
# the published table when none exists — that path is fine to exercise).
PRICING="${PRICING_FILE:-$PWD/.shot-none-pricing.yaml}"
# Size the run itself: `run` spawns the session, so sizing a prior `open`
# would be lost (the PTY would fall back to 80x30).
shell-use run --session "$SESSION" --cols "${COLS:-120}" --rows "${ROWS:-36}" --cwd "$PWD" \
  --env OPENAI_API_KEY="${OPENAI_API_KEY:-test-key}" \
  --env CONFIG_FILE="$CFG" --env PRICING_FILE="$PRICING" \
  ./bin/aibench >/dev/null
shell-use --session "$SESSION" wait text "Ask something" --timeout 8000 >/dev/null
if [ -n "${WAIT_TEXT:-}" ]; then # e.g. WAIT_TEXT="tokens" to catch a finished response
  shell-use --session "$SESSION" wait text "$WAIT_TEXT" --timeout 30000 >/dev/null
fi
if [ -n "${KEYS:-}" ]; then # e.g. KEYS="Shift+Tab Shift+Tab" make tui-shot for the Prompt tab
  for key in $KEYS; do
    case "$key" in
    # shell-use has no name for backtab; send the raw sequence.
    Shift+Tab) shell-use --session "$SESSION" write $'\e[Z' ;;
    *) shell-use --session "$SESSION" press "$key" ;;
    esac
  done
  shell-use --session "$SESSION" wait idle >/dev/null
fi
shell-use --session "$SESSION" text
if [ -n "${SVG_OUT:-}" ]; then # colors matter (e.g. find highlights): eyeball the SVG
  shell-use --session "$SESSION" screenshot "$SVG_OUT" >/dev/null
fi
shell-use close --session "$SESSION" >/dev/null 2>&1
