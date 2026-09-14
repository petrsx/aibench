# Configuration

`aibench.yaml` is where you describe the endpoints you want to test.
One **profile** per endpoint: what to talk to, how to reach it, and
which file holds the secret. The secret lives in a small env-format
file the profile points at, so the yaml stays safe to share;
[authentication](authentication.md) explains what goes in that file.

If you would rather start from something that works, copy from
[`examples/settings/aibench.yaml`](../examples/settings/aibench.yaml)
(one short profile per scenario),
[`examples/settings/.env.example`](../examples/settings/.env.example)
(which variables each auth method wants), and
[`examples/prompts/`](../examples/prompts) (a complete prompt set).

## Common setups

Most people need one of these four. Every field is explained further
down; these are the ones you will reach for first.

**An OpenAI or Anthropic key, straight to the provider:**

```yaml
profiles:
  openai:
    kind: model
    api:
      provider: openai
      base: https://api.openai.com/v1
      model: gpt-5.4-mini
    auth:
      credentials: .env      # OPENAI_API_KEY
```

**An Azure Foundry deployment, by key:**

```yaml
profiles:
  dev:
    kind: model
    api:
      provider: openai
      base: https://your-resource.services.ai.azure.com/openai/v1
      model: gpt-5.4-mini    # the deployment name
      headers:
        api-key: ${FOUNDRY_KEY}
    auth:
      credentials: .env
```

To sign in with your Azure identity, change only the credentials
file; see
[authentication](authentication.md#service-principal-app-identity).

**Claude, direct or on Foundry:**

```yaml
profiles:
  claude:
    kind: model
    api:
      provider: anthropic    # implies the messages wire
      base: https://api.anthropic.com
      model: claude-opus-4-8
    auth:
      credentials: .env      # ANTHROPIC_API_KEY
```

**A hosted agent.** It owns its own instructions, so you send only the
message:

```yaml
profiles:
  my-agent:
    kind: agent              # implies responses + server state, no streaming
    api:
      provider: openai
      base: https://your-resource.services.ai.azure.com/api/projects/p/agents/a/endpoint/protocols/openai
      model: gpt-5.4-mini    # must match the agent's own deployment
      headers:
        api-key: ${FOUNDRY_KEY}
    auth:
      credentials: .env
```

When you want to run one prompt against several of these, declare the
prompt once under `prompts:` and point each profile at it with
`prompt:`. Endpoints and prompts are separate axes, so one prompt and
three endpoints is four entries.

## Where the file lives

The easy way: **run `aibench` and it writes a starter file to
`~/.aibench/aibench.yaml`**, tells you what to fill in, and exits. Edit
that file and run again.

Everything below is for when you want the file somewhere else. aibench
looks in this order and takes the first hit:

| # | POSIX | Windows | Use it for |
|---|---|---|---|
| 1 | `--config <path>`, or the `CONFIG_FILE` env | same | pinning one specific file |
| 2 | `./aibench.yaml` | `.\aibench.yaml` | a config that travels with a project |
| 3 | `./.aibench/aibench.yaml` | `.\.aibench\aibench.yaml` | the same, kept out of the project root |
| 4 | `~/.aibench/aibench.yaml` | `%USERPROFILE%\.aibench\aibench.yaml` | **the default** — your own profiles |

A file in the project (2 or 3) wins over the home one whenever you run
from that folder, so a project can carry its own profiles without any
flag. `/init` inside the app scaffolds that `./.aibench` folder for you.

A path you pin with `--config` is used exactly as given: if the file
is missing you get an error saying so, and the search order above stays
out of it. The starter file is only ever written to the default
location, so a pinned path stays yours to create.

The rest of this page writes paths the POSIX way. On Windows read `~` as
`%USERPROFILE%`; everything else is the same.

### A folder that holds everything

Nothing forces your files into one place, but keeping them together is
what makes a setup easy to move, and it is what the first run creates:

```
.aibench/            # or ~/.aibench, or wherever you keep it
  aibench.yaml       # yours: the profiles
  .env.*             # yours: per-endpoint credentials
  prompts/           # yours: the prompt sets your profiles point at (optional)
  pricing.yaml       # yours: model rates (aibench pricing update; optional)
  settings.json      # the app's: theme, editor, startup profile, and so on
  aibench.log        # the app's, only if you turn logging on
```

**Relative paths in the yaml are taken from the folder you run
`aibench` in.** That goes for `instructions:`,
`starters:`, `tools:` and `credentials:` alike. It is the same rule a
shell uses: standing in the folder above a project, the prompt is
`project/prompts/x.md`; standing inside it, `prompts/x.md`. A config
that has to work from anywhere should use absolute paths.

### What the app writes

`aibench.yaml` is yours alone; the app only reads it. The little it
remembers between runs goes into **`settings.json`** beside it: the theme, which editor
`/profiles-edit` opens, the profile to start on, whether to check for
updates, whether to keep a log, and the last profile you used. You
normally change these from the `/settings` screen, but it is plain JSON
and safe to edit by hand.

`aibench.log` also lands beside the config, and only when you ask for
it: turn it on under App log in `/settings`, or pass `--debug` for one
run. A project with its own yaml therefore gets its own settings and its
own log.

### Editing it

The file is live-reloaded: save it and the running app applies the
change. `/profiles-edit` opens it in your editor from inside the app,
and `aibench profiles edit` does the same from the shell.

Every key is checked. Misspell one and you get an error naming it. For
completion and checking as you type, keep the modeline the starter file
comes with:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/petrsx/aibench/main/schema/aibench.schema.json
```

It points at the schema published from this repo, so your editor has
everything it needs. The line is for the editor only; aibench checks
the file itself. Swap `main` in the URL for a tag or commit if you want
your editor's schema to stay put.

## The whole shape

```yaml
tools:                       # tool files (declarations + bindings), reusable by key
  <key>: weather.tools.json

prompts:                     # what is being tested — a profile picks its set with prompt:
  <key>:
    instructions: weather-assistant.md        # system prompt; omit for a starters-only set
    starters:                                 # 0..n scripts; each file = one conversation
      - weather-assistant.starters.md
    tools: [<key>]                            # 0..n tools keys, merged in order

profiles:                    # where requests go — /profiles switches endpoints
  <name>:
    kind: model | agent      # required, always spelled out
    api:                     # what to speak and where
      provider: openai | anthropic
      type: chat | responses # openai only; anthropic implies messages
      store: true            # responses api only
      stream: false          # responses api only
      base: https://…        # the whole route, may carry {model}
      model: gpt-5.4-mini
      headers: {…}           # verbatim, ${VAR} from the credentials file
      query: {…}             # verbatim
    auth:
      credentials: .env      # env-format file, or "env" for the process env
    prompt: <key>            # optional pin: the prompts key this profile starts on
```

Three top-level maps. `profiles` is where requests go, `prompts` is what
goes with them, `tools` is a pool of tool files the prompt sets share.
Because profiles and prompts are separate, testing N prompts against M
endpoints takes N + M entries.

Within a profile the fields read best in this order: `kind`, `api`,
`auth`, `prompt`; and within `api`: the wire (`provider`, `type`,
`store`, `stream`), then the target (`base`, `model`), then the extras
(`query`, `headers`). The parser does not care, but your future self
will.

## `kind` — what stands behind the endpoint

You always write it. The two kinds behave differently enough that
spelling it out is what keeps a profile readable.

- **`model`** is plain inference. You own the prompt and the params: the
  active set's instructions ride every request, and the Prompt tab's
  params are yours to turn.
- **`agent`** is a hosted agent, Foundry's for example, that keeps its
  instructions and tools on the server and remembers the conversation
  itself. That implies the responses wire and server-side state, and it
  turns streaming off, because the agent endpoint answers a streaming
  request with an empty stream. Writing `api.type` or `api.store` on an
  agent, or pinning a prompt set that carries instructions, gets you an
  error that says which of them the agent already decides. A
  starters-only set is fine, since starters are just messages you send.

Other kinds will appear when the app can actually drive them.

## `api` — the wire and the route

Two things are decided here and they are independent: **what dialect
you speak** (provider and type) and **where you send it** (base).

### The wire

| provider | type | wire | SDK appends | use it for |
|---|---|---|---|---|
| `openai` | `responses` | responses | `/responses` | **new work** — see below |
| `openai` | `chat` (default) | chat completions | `/chat/completions` | existing integrations, wide compatibility |
| `anthropic` | *(omit)* | messages | `/v1/messages` | anything Claude |

**Start new work on `responses`.** It is the current generation of the
openai wire and the only one that carries the extras: server-side state
(`store: true`, each request chaining on `previous_response_id`), the
`stream` switch, and the shape hosted agents speak. `chat` is not going
anywhere and stays the interoperable default, since it is what most
gateways and non-OpenAI providers implement. Both are supported to the
same standard, and flipping a profile between them is the intended way
to see how one endpoint behaves on each.

**`store: true`** (responses only) makes the endpoint keep the
conversation: each request carries only the new turns and chains on the
previous response id. The default is to replay the whole history in
every request, which is what makes each one fully inspectable in the
Inspector.

**`stream`** (responses only) is the api's own stream parameter. The
default follows the kind: a model streams, an agent does not. Either of
these on the chat or messages wire is an error, because those wires
have no such knob.

### The route is separate from the wire

**The whole route is `api.base`**, and it is independent of which wire
you speak. The SDK appends the operation path from the table above
to whatever you write, so the same `type:` reaches either generation of
an Azure or Foundry endpoint:

| route | base spelling | versioning |
|---|---|---|
| **v1 surface** (current) | `…/openai/v1` | implicit — send no `api-version` |
| **classic deployment route** (legacy) | `…/openai/deployments/{model}` | explicit `api-version` in `api.query` |

```yaml
# classic deployment route
base: https://your-resource.openai.azure.com/openai/deployments/{model}
model: gpt-5.4-mini      # fills {model}
query:
  api-version: 2024-06-01
```

`{model}` is filled from `api.model` before the SDK appends its path,
and anything like `api-version` rides in `api.query`.

> **The classic deployment route is supported for testing and
> compatibility only.** It works — there is a request-shape test pinning
> the path, the `{model}` fill, and the query — but treat it as a probe
> for endpoints that still serve it, not a foundation to build on. Point
> new work at the v1 surface, and keep a classic profile beside it only
> when you want to watch the two generations diverge. Nothing in aibench
> special-cases it: it is a base URL, so if it changes or disappears, it
> changes in your yaml and not in this codebase.

The Assistants API (threads and runs) is not supported. It was a
different protocol from all three wires and has been retired; those
workloads belong on `responses` or on a hosted agent.

### `model` — optional in the parser, needed in practice

Nothing fills `api.model` in for you. Leave it out and the request goes
out with an empty model, which the endpoint either rejects or answers
with its own default. Set it. It does three jobs:

- **It is the request body's `model`.** On Azure and Foundry that means
  the deployment name.
- **It fills `{model}` in `api.base`**, whenever the base carries the
  placeholder.
- **It is the pricing key.** The Metrics panel's cost rows look this
  value up by exact string in `pricing.yaml`, and show only when a row
  matches. If your deployment name differs from the catalog id, add a
  row under the deployment name; `aibench pricing
  update` keeps rows you add by hand ([pricing](pricing.md)).

On an agent endpoint the model must also match the agent's own
deployment, or the endpoint answers 400.

## `auth` — which file holds the secret

`auth.credentials` names an env-format file with only the variables
this profile's method needs, or the literal `env` to read the process
environment, which suits a CI job that already exports them. Which
method is in play follows from which variables the file defines:

| In the credentials file | Effect |
|---|---|
| `OPENAI_API_KEY` | `Authorization: Bearer` (openai's native form) |
| `ANTHROPIC_API_KEY` / `AZURE_API_KEY` | `x-api-key` (anthropic's native form) |
| `AZURE_TOKEN` | a token you minted → `Authorization: Bearer`, sent as-is |
| `AZURE_USE_LOGIN=true` | mint from your own `az login`, fresh every run |
| `AZURE_TENANT_ID` + `CLIENT_ID` + `CLIENT_SECRET` | an app registration's identity → Bearer token |
| anything else | referenced by name from `api.headers` |

Define exactly one of these per file, so you always know which
credential went out; two gets you an error naming both. `api.headers`
composes on top for the schemes a provider has no native form for: a
classic Azure `api-key`, an APIM subscription key.

```yaml
api:
  headers:
    api-key: ${ENDPOINT_KEY}
auth:
  credentials: .env.endpoint
```

aibench sends only the credential you pointed it at, and only from the
file you named. The full story, including how to use your own Azure identity and what
each failure message means, is [authentication](authentication.md).

## `prompts` — what goes with every request

A prompt set is what you are testing, and it belongs to the profile
that names it. `prompt:` on a profile picks its set; a profile without
one gets the first set by name. Switching profile therefore switches
the prompt too and clears the conversation, since a different system
prompt takes over. There is no separate prompt switch: choosing an
endpoint is choosing what it is tested with.

A set has three optional parts:

- **`instructions`** is the prompt file: a markdown body with optional
  YAML frontmatter carrying the request params. On the responses wire it
  is literally the instructions field; on chat and messages it lands as
  the system message. It hot-reloads, and you edit the body in the file,
  not in the app. Leave it out for a starters-only set, which is the only
  shape an agent profile may pin. The file format, params included, is
  [prompts](prompts.md).
- **`tools`** is a list of keys into the top-level `tools:` map, so one
  tool file can serve several sets: a shared websearch beside a set's
  own weather API. Files merge in list order; `tools` arrays concatenate
  and any other collision is an error naming both files. Not valid on a
  set an agent pins, because the agent owns its tools. What goes in a
  tool file is [the tool catalog](tool-catalog.md).
- **`starters`** is a list of starter scripts, one scripted conversation
  per file: markdown with one `## title` heading per prompt. Declaring
  any makes launch offer the `/starters` menu; enter runs the highlighted
  script, esc skips, and **nothing ever auto-sends**. The same scripts
  seed the composer's ↑ history and play headless with `aibench send
  --script`. Valid on every kind.

**A declared file that is missing is reported the moment the profile is
selected**: a notice names it, the Prompt tab keeps saying it, and the
CLI refuses the send. You find out before the request goes out, at the
point where you can fix it.
