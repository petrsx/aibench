#!/usr/bin/env bash
# Record a GIF of a scripted TUI operation with vhs (demos, README,
# PR walkthroughs). Run via `make tui-gif`; needs `brew install vhs`.
#
# One tape = one scripted operation; tapes live in scripts/vhs/. A tape
# writes its gif wherever its own Output line says: docs/media/ for the
# ones the README embeds (committed), bin/ for scratch recordings
# (gitignored, cleaned by `make clean`).
#
# Knobs (all env vars):
#   TAPE           tape to record, by name or path (default: demo)
#   CONFIG_FILE    a profiles yaml to load (default: the demo fixtures)
#   PROMPT_FILE    a prompt file to load (default: the profile's own)
#   PRICING_FILE   a pricing yaml to load (default: the demo rates)
#   MOCK_ADDR      where the stand-in endpoint listens (default 127.0.0.1:8099)
set -eu
cd "$(dirname "$0")/.."

command -v vhs >/dev/null || { echo "vhs not found — brew install vhs" >&2; exit 1; }

TAPE="${TAPE:-demo}"
[ -f "$TAPE" ] || TAPE="scripts/vhs/$TAPE.tape"
[ -f "$TAPE" ] || { echo "no such tape: $TAPE (see scripts/vhs/)" >&2; exit 1; }

# A recording must never touch the developer's own profiles — they point
# at the live gateway, and their names would be baked into a shareable
# gif. Everything below points at the demo fixtures instead, and at
# scripts/mockendpoint, which streams a canned reply so a gif can show a
# real send (streaming text, a record line, usage) without a backend.
MOCK_ADDR="${MOCK_ADDR:-127.0.0.1:8099}"
# A previous recording still holding the port would leave this one talking
# to a mock it does not control — or to nothing. Say so rather than
# producing a gif of a failed send.
if (exec 3<>"/dev/tcp/${MOCK_ADDR%%:*}/${MOCK_ADDR##*:}") 2>/dev/null; then
    echo "$MOCK_ADDR is already in use — another recording may still be running" >&2
    exit 1
fi
go build -o bin/mockendpoint ./scripts/mockendpoint
./bin/mockendpoint -addr "$MOCK_ADDR" >/dev/null 2>&1 &
MOCK_PID=$!
# No exec below: the trap (set once the stage exists) has to survive to
# reap the mock.

# Don't start recording against a port that isn't listening yet — the
# first send of the tape would be the one request that fails.
for _ in $(seq 40); do
    (exec 3<>"/dev/tcp/${MOCK_ADDR%%:*}/${MOCK_ADDR##*:}") 2>/dev/null && break
    sleep 0.05
done

# The recording runs from a throwaway copy of the fixtures, for two
# reasons: the app writes settings.json beside its config (active
# profile, theme), which must not land in the repo — and every recording
# then starts from the same state, where a tape that switches profile
# would otherwise move where the next one begins.
#
# Only the yaml is staged: paths inside it resolve against the working
# directory, which for a recording is the repo root, so it names the
# shipped examples/prompts files directly and what a gif shows is what a
# new user is handed.
STAGE="$(mktemp -d)"
trap 'kill "$MOCK_PID" 2>/dev/null || true; rm -rf "$STAGE"' EXIT
cp scripts/vhs/demo.yaml "$STAGE/"

export OPENAI_API_KEY="${OPENAI_API_KEY:-demo-key}"
export CONFIG_FILE="${CONFIG_FILE:-$STAGE/demo.yaml}"
export PRICING_FILE="${PRICING_FILE:-$PWD/scripts/vhs/demo-pricing.yaml}"
if [ -n "${PROMPT_FILE:-}" ]; then
    export PROMPT_FILE
fi

vhs "$TAPE"
