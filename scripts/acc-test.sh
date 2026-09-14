#!/usr/bin/env bash
# Acceptance test: one real send through each profile against the LIVE
# endpoints — the real config (search order / CONFIG_FILE), the real
# credentials. This is the deliberate opposite of the TUI tests' isolation:
# it exists to prove the profiles work end-to-end. Developer-run only
# (make acc-test) or via the manual acc-test workflow (workflow_dispatch,
# credentials from the ACC_BUNDLE secret); never in the PR merge gate and
# never run by agents.
#
# Knobs:
#   PROFILES  space-separated subset to test (default: all, via `aibench profiles list`)
#   MSG       the message to send (default below)
#   TIMEOUT   per-send abort (default 90s)
set -u

BIN=${BIN:-bin/aibench}
MSG=${MSG:-"Connection test: reply with the single word ok."}
TIMEOUT=${TIMEOUT:-90s}

if [ -n "${PROFILES:-}" ]; then
  profiles=$PROFILES
else
  profiles=$("$BIN" profiles list) || { echo "acc: no profiles to test" >&2; exit 1; }
fi

pass=0 fail=0 failed=""
for p in $profiles; do
  printf '── %s\n' "$p"
  if out=$("$BIN" send --profile "$p" --timeout "$TIMEOUT" "$MSG" 2>&1); then
    printf '%s\n' "$out" | sed 's/^/   /'
    pass=$((pass + 1))
  else
    printf '%s\n' "$out" | sed 's/^/   /'
    printf '   FAIL\n'
    fail=$((fail + 1)) failed="$failed $p"
  fi
done

echo
echo "acc: $pass passed, $fail failed${failed:+ (${failed# })}"
[ "$fail" -eq 0 ]
