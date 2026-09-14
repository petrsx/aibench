# The command line

aibench is a TUI first; the commands here are the secondary surface —
for smoke-testing a profile, scripting a check in CI, and the small
housekeeping the app does for itself.

They read the same config as the app: same profiles, same prompt sets,
same credentials files ([configuration](configuration.md),
[authentication](authentication.md)). Everything here runs without the
TUI, and `aibench send` *is* the app's send with no screen: the same
history, the same tool loop, the same requests.

```console
$ aibench                       # the app
$ aibench send "hello"          # one request, through the active profile
$ aibench profiles edit         # open aibench.yaml in your editor
$ aibench pricing update        # fetch current model rates
```

Two flags apply everywhere:

| Flag | Does |
| --- | --- |
| `--config <file>` | use this config file; a missing one is an error |
| `--debug` | write a detailed log for this run — see [the app log](#the-app-log) |

## `send` — one request, out loud

The reason the CLI exists: prove a profile works, or fold a real request
into a script.

```console
$ aibench send "what is the weather in Paris"
$ echo "summarise this" | aibench send
$ aibench send -p prod "ping"
$ aibench send --timeout 90s "a question worth waiting for"
```

| Flag | Does |
| --- | --- |
| `-p`, `--profile <name>` | send through this profile (default: the active one) |
| `--timeout <duration>` | abort the request after this long, e.g. `90s` (default: none) |
| `--script [name]` | play a starter script instead of a message — see below |

The message comes from the arguments, or from **stdin** when there are
none, so `aibench send` composes with everything else in a pipeline.

**The reply streams to stdout; everything else goes to stderr** — the
summary line (profile, elapsed, tokens) and any tool calls the model
asked for. Redirecting stdout therefore gives you the answer and nothing
else:

```console
$ aibench send "name three cities" > answer.txt
gpt-5.4-mini · 0.8s · tokens in 96 out 34
```

**Bound tools run here too.** A call the model makes to a tool with a
binding is executed, its result sent back up, and the final reply is
what lands on stdout — the same loop the app runs, with each round
noted on stderr (`tool get_stations: 21 bytes`). A tool without a
binding is reported there and the run ends, as in the app. Use the TUI
when you want to watch each round's request and response
([tools](tool-catalog.md)).

### `--script` — a scripted conversation

Plays the active prompt set's starter script: each prompt sent after the
previous reply came back, so later turns carry the earlier ones as
context. Bare `--script` takes the first script; `--script <name>` picks
one.

```console
$ aibench send --script
> What can you help me with?
I can check current conditions and short-term forecasts…
mini · gpt-5.4-mini · 0.9s · tokens in 96 out 40
> And is that good weather for a run?
Good conditions for it after 18:00, once the rain clears…
mini · gpt-5.4-mini · 0.8s · tokens in 463 out 34
```

User turns are echoed with a `> ` prefix when the script has more than
one prompt, so the output reads as a transcript. This is the headless
twin of `/starters` — the same file, the same order, the same requests
([prompts](prompts.md#starter-scripts)).

## `profiles edit` — open the config

Everything about where requests go lives in `aibench.yaml`, and you
write that file yourself; the app reads it and live-reloads it. When
you want to add a profile or fix a base URL
without leaving the terminal:

```console
$ aibench profiles edit
```

It opens the file in your editor — the one you picked in the app's
`/settings` screen, or failing that whatever `$VISUAL` or `$EDITOR` says.
This is the same editor `/profiles-edit` opens inside the app, so you only set it
once. If none of those is set, you get a message saying which to set.
The file
doesn't have to exist yet, so this is also a fine way to write your
first config.

## `pricing` — model rates

The cost rows in the Metrics panel come from a `pricing.yaml` next to
your config ([pricing](pricing.md)). Once you have that file it is the
only source of rates, and these two commands are how it comes to be and
stays current:

```console
$ aibench pricing update    # pull the latest rates from catwalk into your file
$ aibench pricing edit      # open the file and add a row by hand
```

`update` is a merge: models catwalk knows get added or
refreshed, and rows only you have stay put. That is what makes `edit`
useful — an Azure deployment name or an internal gateway model is
yours alone, no registry will ever list it, and a row you add by hand
survives every later update. `--out <file>` writes somewhere else;
`--url <address>` (or `CATWALK_URL`) pulls from a different registry.

Both work while the app is open in another pane: `pricing.yaml`
hot-reloads like the config and prompt files, so the cost rows reprice
the moment the file is saved. Inside the app, `/pricing-update` runs
the same refresh.

## The app log

aibench keeps a log only if you ask for it. Turned on, it writes
`aibench.log` next to the config file in use — so a project with its own `aibench.yaml` gets its own log too.
Switch it on for good under App log in `/settings`, or just for one run
with `--debug`, which also turns up the detail:

```console
$ aibench --debug           # this run logs, whatever the setting says
$ tail -f aibench.log       # it's right beside aibench.yaml
```

Credentials are never written to it.

## Exit status

Zero when the request completed, non-zero on any failure — a config that
does not resolve, credentials that do not satisfy the
[auth contract](authentication.md#when-it-fails), a timeout, or an error
from the endpoint. Errors go to stderr in one line, so a CI step fails
with the reason in its log.
