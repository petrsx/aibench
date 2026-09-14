# Tool catalog

A tool is a function you declare to the model and, optionally, teach
aibench to execute. Both halves live in a **tool file**, declared in
`aibench.yaml`'s top-level `tools:` map and referenced by prompt sets —
so a prompt-specific file and a shared one (websearch) ride the same
mechanism, and every referenced file hot-reloads:

```yaml
tools:
  weather-api: weather.tools.json
  websearch: websearch.tools.json

prompts:
  weather:
    instructions: weather-assistant.md
    tools: [weather-api, websearch]   # merged in list order
```

Worked example: [`examples/prompts/weather-assistant.tools.json`](../examples/prompts/weather-assistant.tools.json),
which runs end to end with no API key.

## The catalog

Every tool a model can call falls into one of these. The first four are
declared by you and executed (or deliberately not) by aibench; the last is
what a hosted agent's own tools look like from here.

| Type | Executes where | Needs | Reach for it when |
| --- | --- | --- | --- |
| [`http`](#http--call-a-real-api) | aibench, in-process | a reachable URL | the tool is a real API call |
| [`static`](#static--a-canned-result) | aibench, no I/O | nothing | the *result* must be identical every run |
| [`mcp`](#mcp--call-an-mcp-server) | an MCP server you point at | a streamable-HTTP endpoint | someone else already hosts the tool |
| [*(no binding)*](#no-binding--inspect-only) | nowhere — the call is only displayed | nothing | trying out a tool idea: check if and how the model would call it, before building it |
| *server-side* | the provider's service | a `kind: agent` profile | the agent owns its tools (web search, code interpreter, file search, its own MCP/OpenAPI connections) |

The last row is not something you configure here: a hosted agent's tools
are attached to the agent, so aibench declares none and simply shows the
calls and results as they come back. Everything above it is yours, and the
dividing line holds throughout — **aibench executes what the client would
execute, and shows what the server does.**

### Where this sits in the wider vocabulary

Platform catalogs — [Foundry's](https://learn.microsoft.com/azure/foundry/agents/concepts/tool-catalog),
OpenAI's — sort tools by *what kind of thing* the tool is: web search, code
interpreter, file search, **function calling**, MCP, OpenAPI, A2A. In that
taxonomy, everything on this page is the single entry **function calling**
— *"define custom functions the agent can call; your application executes
them and returns the result."* That is precisely aibench's job.

So this catalog sorts on the axis below theirs: given that the model is
calling a function, **how is that call executed?** The two are not
alternatives, they are different depths:

```
function calling  ← the wire-level tool type; the only one aibench declares
├── http          ← how aibench executes the call
├── static
├── mcp
└── (unbound)     ← it does not
```

One consequence is worth internalising: **the binding type is invisible to
the model.** It sees `get_weather(latitude, longitude)` and a result,
whether that came from a live API, a canned string, or an MCP server. That
is what makes `static` a legitimate substitute for `http` when you are
A/B-testing a prompt — swapping the executor changes nothing the model can
perceive except the result itself.

Note the collision this creates around MCP: it is a *tool type* in a
platform catalog (the agent connects to a server itself) and an *executor*
here (the model calls a function, aibench forwards it to a server). Same
protocol, different layer.

## The file

```jsonc
{
  "chat":      { "tools": [ … ], "tool_choice": "auto" },  // sent verbatim
  "responses": { "tools": [ … ] },                         // sent verbatim
  "messages":  { "tools": [ … ] },                         // sent verbatim
  "bindings":  { "get_weather": { … } }                    // aibench-only, never sent
}
```

**Multiple files merge in the order the prompt set lists them**: each
wire section's `tools` arrays concatenate; any *other* field two files
both set (`tool_choice`, `response_format`, a tool name bound twice) is
an error naming both files. A `"//"` key is a
comment and is skipped. A declared file that is missing is an error too:
the declaration is the intent.

**One section per wire, merged byte-for-byte.** The section matching the
active profile's api is merged into the request body exactly as written;
the others are ignored. Nothing is translated, because the three wires
genuinely disagree on shape:

```
chat       {"type":"function","function":{"name":…,"parameters":…}}
responses  {"type":"function","name":…,"parameters":…,"strict":true}
messages   {"name":…,"input_schema":…}
```

Translating would mean inspecting *aibench's* idea of your request instead
of your own. The cost is that **keeping the sections in sync is your job**:
if `chat` declares three tools and `responses` one, switching profiles
changes the tool set, and any comparison between the two is meaningless.

A section can carry anything the wire accepts, not only tools —
`response_format`, `parallel_tool_calls`, and so on ride the same way.

**`bindings` is aibench's own**, keyed by tool name, and is stripped
before the request goes out. It says how to *execute* a call.

## Executors — how a function call runs

Which one runs is the **binding's** `type`. It is optional: omitted means
`http`. An unrecognised value is an error naming it.

> **Two different `type` fields, and only one of them is optional.** The
> `"type": "function"` inside a tool *declaration* belongs to the wire —
> the provider requires it, it is sent verbatim, and dropping it breaks the
> request. The `type` inside a `bindings` entry is aibench's own executor
> selector, never sent, and defaults to `http`. They sit in the same file
> and mean nothing to each other:
>
> ```jsonc
> "chat": { "tools": [
>   { "type": "function",                  // the wire's — required
>     "function": { "name": "get_weather", … } } ] },
> "bindings": {
>   "get_weather": { "type": "http", … }   // aibench's — optional
> }
> ```

### `http` — call a real API

Written here without a `type`, to show the default; the shipped example
spells `"type": "http"` out, which reads better beside a `static` or `mcp`
entry.

```json
"get_weather": {
  "url": "https://api.open-meteo.com/v1/forecast",
  "query": { "latitude": "{latitude}", "longitude": "{longitude}",
             "current": "temperature_2m,wind_speed_10m" },
  "pick": "current"
}
```

| field | meaning |
| --- | --- |
| `url` | request URL; `{arg}` placeholders fill from the call's arguments, path-escaped |
| `method` | default `GET` |
| `query`, `body` | same `{arg}` filling; a parameter whose argument is absent is left out of the request |
| `headers` | values expand `${VAR}` from the **process environment** — see the note below |
| `pick` | a [gjson](https://github.com/tidwall/gjson) path applied to a JSON response |

`pick` matters more than it looks: models pay for every token of a tool
result, and a raw forecast document is mostly metadata. `pick: "current"`
turns it into the four fields the answer needs.

> **Header secrets come from your shell environment.** The profile's
> credentials file serves only its own endpoint's auth, so export what a
> binding needs:
> `export RAIL_API_KEY=… && aibench`.

### `static` — a canned result

```json
"search_locations": { "type": "static",
                      "result": [{"id":"paris-fr","name":"Paris, France"}],
                      "delay_ms": 400 }
```

`result` is any JSON (a JSON *string* unwraps to bare text; anything else
is compacted). `error` instead of `result` makes the call fail with that
message. `delay_ms` simulates latency.

This is the executor for **prompt A/B testing**: with the tool result
pinned, a change in the reply is attributable to the prompt and nothing
else. It is also how you rehearse failure — point `error` at a plausible
message and watch whether the assistant admits the failure or invents an
answer.

### `mcp` — call an MCP server

```json
"search_docs": { "type": "mcp", "server": "http://localhost:3001/mcp", "tool": "search" }
```

`server` is a **streamable-HTTP** MCP endpoint; `tool` overrides the
remote name when it differs from the one you declared. One short-lived
session per call — nothing is shared between concurrent calls.

**stdio / spawned servers are not supported.** Process lifecycle does not
belong in a file that hot-reloads.

### No binding — inspect-only

A declared tool with no binding entry is a mode in its own right: the
model's call renders in the transcript with its arguments, and that is
all that happens. Use it to answer *"would the model call this, and
with what?"* before building the tool at all. `get_alerts` in the example
is deliberately left this way.

## What applies to every call

- **A 30-second timeout** bounds each call, so a dead endpoint turns
  into an error result the model can react to.
- **Results are capped.** An over-budget JSON *array* is cut at element
  boundaries with a marker element appended, so the model always receives
  valid JSON and can tell it is partial; anything else is cut by bytes.
- **Each round of tool calls is its own record** in the Inspector, so
  you can read every request the loop made. A send is capped at 10
  rounds.

## Not supported

These exist in other stacks and are deliberately absent here:

| | why not |
| --- | --- |
| **OpenAPI tools** — hand a spec, get tool definitions | aibench declares tools verbatim in each wire's shape; generating them from a spec would put a translator between you and the request. A hosted agent can use one (Foundry's OpenAPI tool) — it just runs server-side, where aibench sees only the resulting calls. An offline `aibench tools import` that *generates* tools and http bindings from a spec is on the [roadmap](../CONTRIBUTING.md#roadmap): generation you can review in a diff keeps the file on disk the thing that goes on the wire. |
| **Hosted tools** — web search, code interpreter, file search | These execute inside the provider's service. On a `kind: agent` profile they already work; they are simply not something a client declares or runs. |
| **stdio MCP servers** | Process lifecycle in a hot-reloading file (see above). |
| **A2A (agent-to-agent)** | Server-side wiring on the agent, like the hosted tools. |

The dividing line is consistent: **aibench executes what the client would
execute, and shows what the server does.** For a `kind: agent` profile the
agent owns its tools entirely, which is why such profiles have no prompt
file and therefore no tools file.
