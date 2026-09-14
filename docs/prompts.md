# Prompt files

A prompt file is the thing you are testing: **one markdown file holding
the instructions and the settings they are tested with.** Point a profile
at it and every send uses it; edit it and the running app picks it up.

```markdown
---
reasoning.effort: medium
text.verbosity: low
---

# Weather Assistant

You are a concise weather assistant. You help users check current
conditions for a place they name, and nothing else.
```

Two parts, and that is the whole format:

- the **frontmatter** between `---` lines — the [params](#params--the-knobs)
- the **body** — the system prompt, in markdown, sent as written

Neither part is required. A file with no frontmatter is just a prompt; a
file with no body is a set of params (useful for a starters-only set).

## Where it lives, and who points at it

A prompt file is named by a prompt set in your `aibench.yaml`, and a
profile picks the set:

```yaml
prompts:
  weather:
    instructions: weather-assistant.md      # this file
    tools: [weather]                        # optional, see below
    starters: [weather-assistant.starters.md] # optional, see below

profiles:
  gpt5-mini:
    kind: model
    api: {provider: openai, base: …, model: gpt-5.4-mini}
    prompt: weather                         # the set this endpoint runs
```

Paths are taken from the folder you run `aibench` in. The full config
model is in
[`configuration.md`](configuration.md); everything below is about the
file itself.

## Editing it while the app runs

Both directions are live:

- **Edit the file** in your editor — the app is watching, and reloads
  when you save. The Prompt tab re-renders and the next send uses it.
- **Edit the params in the app** — type in the Prompt tab's Params panel
  and the file is written a second later. The panel lists the params your
  file defines: emptying a row removes it from the file, and adding one
  is a line of frontmatter.

The body is only ever edited in the file. The app renders it, and
rendering is not editing.

## Params — the knobs

Params are the knobs on a request: how adventurous the model may be,
how long it may think, how much it should write. They sit in the
frontmatter above the prompt, and you turn them in the file or in the
Prompt tab's Params panel, whichever is closer to hand.

A few things are worth knowing before you add one:

- **Leave a param out and the model's own default applies.** The
  request goes without it, so you are testing the endpoint exactly as it
  ships.
- **The tables below are the complete list of what gets sent.** A name
  outside them stays in your file, and the Params panel marks it, so a
  typo shows up right away.
- **Whether a particular model accepts a param is the endpoint's call.**
  A reasoning model turns down `temperature` and wants
  `reasoning.effort`; set one it will not take and the endpoint's own
  error tells you, verbatim, in the Inspector.

Which table applies follows from your profile's `api`, so it changes
with the provider you point at.

**Provider: openai** (and any gateway speaking its wires)

### `api: chat` — Chat Completions

| Param | What it does | Values |
| --- | --- | --- |
| `temperature` | randomness | `0`–`2` |
| `top_p` | nucleus sampling, instead of temperature | `0`–`1` |
| `max_tokens` | cap on the reply | whole number |
| `reasoning_effort` | how much thinking a reasoning model spends | `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` |
| `verbosity` | how much prose comes back | `low`, `medium`, `high` |
| `frequency_penalty` | discourage repeated tokens | `-2`–`2` |
| `presence_penalty` | discourage repeated topics | `-2`–`2` |
| `seed` | best-effort reproducibility | whole number |
| `stop` | stop sequences | comma-separated, e.g. `END,###` |

### `api: responses` — Responses

Same two reasoning knobs, spelled the way this api nests them.

| Param | What it does | Values |
| --- | --- | --- |
| `temperature` | randomness | `0`–`2` |
| `top_p` | nucleus sampling, instead of temperature | `0`–`1` |
| `max_output_tokens` | cap on the reply | whole number |
| `reasoning.effort` | how much thinking a reasoning model spends | `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` |
| `text.verbosity` | how much prose comes back | `low`, `medium`, `high` |

This api takes no penalties, seed, or stop sequences.

**Provider: anthropic**

### `api: messages` — Messages

| Param | What it does | Values |
| --- | --- | --- |
| `temperature` | randomness | `0`–`1` |
| `top_p` | nucleus sampling, instead of temperature | `0`–`1` |
| `top_k` | sample from the k likeliest tokens | whole number |
| `max_tokens` | cap on the reply | whole number (defaults to `4096`, which this api requires) |
| `stop` | stop sequences | comma-separated, e.g. `END,###` |

Anything else a request needs — tool declarations, a structured-output
schema, an api's own extras — goes in the set's
[tool file](tool-catalog.md), which merges into the request body verbatim.

## Starter scripts

A starter script is a conversation you want to be able to run again: a
markdown file of canned **user** messages, one per `##` heading.

```markdown
## weather in Paris

What's the weather in Paris, France?

## and for a run

Is that good weather for a run?
```

Name them in the set's `starters:` list — one file is one conversation,
and a set may have several. Declaring any makes launch **offer** the
`/starters` selection; enter runs the highlighted script, esc skips, and
**nothing ever auto-sends**. The same scripts seed the composer's ↑
history and play headless with `aibench send --script`.

This is also where a conversation worth repeating belongs: conversations
themselves are deliberately not saved.

## Tools beside the prompt

An optional **`<prompt>.tools.json`** next to the prompt file declares the
tools the model may call, in each api's own wire shape, plus how aibench
should actually run them. It merges into the request byte for byte.
Full reference: [`tool-catalog.md`](tool-catalog.md).

## A worked example

[`examples/prompts/weather-assistant.md`](../examples/prompts/weather-assistant.md)
with its [tool file](../examples/prompts/weather-assistant.tools.json) and
[starter script](../examples/prompts/weather-assistant.starters.md) — a working set
you can copy and point a profile at.
