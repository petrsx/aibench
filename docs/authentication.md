# Authentication

How aibench proves who it is to an endpoint. The companion principle to
the config split: **`aibench.yaml` profiles say *what to talk to*; the
profile's credentials file says *how to authenticate*.** Endpoint config
never lives in env; secrets never live in yaml.

## Auth methods

Each method links to its guidance below.

**Provider: native**

| Method | Endpoint | Variables |
| --- | --- | --- |
| [OpenAI API key](#provider-native-openai-anthropic) | `api.openai.com` | `OPENAI_API_KEY` |
| [Anthropic API key](#provider-native-openai-anthropic) | `api.anthropic.com` | `ANTHROPIC_API_KEY` |

**Provider: Foundry**

| Method | Endpoint | Variables |
| --- | --- | --- |
| [OpenAI deployment key](#api-key--openai-endpoint) | OpenAI endpoint | your choice, e.g. `MY_KEY` |
| [Claude deployment key](#api-key--claude-endpoint) | Claude endpoint | `AZURE_API_KEY` |
| [Entra ID — service principal](#service-principal-app-identity) | any | `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET` |
| [Entra ID — bring your own token](#bring-your-own-token-byot) | any | `AZURE_TOKEN` |
| [Entra ID — sign-in](#sign-in-azure_use_login) | any | `AZURE_USE_LOGIN=true` |

**Gateways**

| Method | Endpoint | Variables |
| --- | --- | --- |
| [Custom header](#gateways-apim) | APIM, … | your choice, e.g. `SUBSCRIPTION_KEY` |

## The credentials file

Each profile's `auth:` group names its credentials source:

```yaml
auth:
    credentials: .env.dev   # env-format file, resolved from where you run aibench
    # or — credentials: env — the process environment alone
```

Keep **one file per endpoint** (`.env.dev`, `.env.prod`, …), each holding
only the secrets that endpoint's auth method needs — the file's values are
used only while its profile is active, so one endpoint's secrets never
reach another's requests.

Anything a file does not define falls back to your shell environment, and
`credentials: env` uses the shell alone.

## Provider: native (OpenAI, Anthropic)

One method: the API key the provider issued, in its standard variable.
Nothing else to configure — aibench sends it in the provider's native
form.

| Provider | Variable | Sent as |
| --- | --- | --- |
| `openai` | `OPENAI_API_KEY` | `Authorization: Bearer` |
| `anthropic` | `ANTHROPIC_API_KEY`, else `AZURE_API_KEY` | `x-api-key` |

The anthropic wire reads both names so the same profile shape works
direct and on Foundry; with both defined, `ANTHROPIC_API_KEY` wins. They
are one method, not two — defining both is not a conflict.

```yaml
plain:
    kind: model
    api:
        provider: openai
        base: https://api.openai.com/v1
        model: gpt-5.4-mini
    auth:
        credentials: .env.plain    # OPENAI_API_KEY → Authorization: Bearer
```

```dotenv
# .env.plain
OPENAI_API_KEY=…
```

## Provider: Foundry

A Foundry deployment authenticates with **a key or Entra ID — pick one
per profile** (defining two methods is a validation error; there is no
precedence to learn).

### API key — OpenAI endpoint

The endpoint wants the key in an `api-key` *header*, so the profile maps
it: the header name is fixed, the `${…}` variable name is yours — pick
one and define it in the credentials file.

```yaml
azure-key:
    kind: model
    api:
        provider: openai
        base: https://my-resource.services.ai.azure.com/openai/v1
        model: gpt-5.4-mini
        headers:
            api-key: ${MY_KEY}   # the fixed header name ← your variable
    auth:
        credentials: .env.azure
```

The same `headers:` block works whatever route the base names — including
the classic `…/openai/deployments/{model}`, which additionally needs an
`api-version` in `query:` ([routes](configuration.md#the-route-is-separate-from-the-wire)).

```dotenv
# .env.azure
MY_KEY=…
```

### API key — Claude endpoint

Claude on Foundry (`provider: anthropic`) takes its key through the
built-in `AZURE_API_KEY` — the variable name a Foundry deployment issues
— sent natively as `x-api-key`, no header mapping. The profile looks
exactly like the [native example](#provider-native-openai-anthropic) with
`AZURE_API_KEY=…` in the credentials file.

### Entra ID

No key: auth is a bearer token, and the choice is *whose identity* —

- **[a service principal](#service-principal-app-identity)** — an app
  registration's identity: CI, automation, shared endpoints;
- **[a token you mint yourself](#bring-your-own-token-byot)** (BYOT) —
  your identity, exactly the bearer you pasted goes on the wire: the
  honest choice while debugging auth;
- **[your sign-in, minted per run](#sign-in-azure_use_login)** — your
  identity, fresh token every run, nothing to paste: the everyday choice
  once auth works.

Put Entra variables **only in the credentials files of the endpoints
that should use them.** A profile sees its own file *and* your shell
through the fallback, so an `AZURE_*` set exported in your shell reaches
every profile: one whose file holds no key will authenticate with it,
and one whose file holds a key fails as a conflict. Both are surprises
that start outside the file you are reading.

The **token audience** is derived from the route (a mismatch is a bare
401, so it is not left to configuration):

| Profile speaks | Audience |
| --- | --- |
| `type: responses`, or `kind: agent` | `https://ai.azure.com/.default` |
| everything else | `https://cognitiveservices.azure.com/.default` |

#### Service principal (app identity)

Shown on a Foundry resource; a classic
`my-resource.openai.azure.com` host works exactly the same.

```yaml
sp:
    kind: model
    api:
        provider: openai
        type: responses
        # the v1 surface; the SDK appends /responses
        base: https://my-resource.services.ai.azure.com/openai/v1
        model: gpt-5.4-mini
    auth:
        credentials: .env.sp
```

```dotenv
# .env.sp — app registration with a role on the resource.
# No OPENAI_API_KEY here: two methods in one file is a conflict error.
AZURE_TENANT_ID=…
AZURE_CLIENT_ID=…
AZURE_CLIENT_SECRET=…
```

#### Bring your own token (BYOT)

Put the bearer in `AZURE_TOKEN` — a first-class method, no `headers`
line needed. aibench adds the `Bearer ` prefix and never refreshes the
token (deliberate: it acquires no credential you didn't hand it); when a
request comes back 401, mint a fresh one. Typically expires in an hour.

```yaml
token:
    kind: model
    api:
        provider: openai
        type: responses
        base: https://my-resource.services.ai.azure.com/openai/v1
        model: gpt-5.4-mini
    auth:
        credentials: .env.token
```

```dotenv
# .env.token — the token itself, nothing else
AZURE_TOKEN=eyJ0eXAiOiJKV1QiLCJhbG...
```

Sign in first:

```sh
az login
```

Then mint the token into the credentials file — the `--resource` must
match the audience table above:

```sh
echo "AZURE_TOKEN=$(az account get-access-token --resource https://ai.azure.com --query accessToken -o tsv)" > .env.token
```

For chat and the classic deployment routes, use
`--resource https://cognitiveservices.azure.com` instead.

#### Sign-in (AZURE_USE_LOGIN)

`AZURE_USE_LOGIN=true` lets aibench mint the token from your `az login` /
`azd auth login` session, fresh every run — an explicit opt-in, never a
fallback. Shown on a Foundry **agent endpoint**, which never offers key
auth:

```yaml
agent:
    # kind agent: responses wire + server-managed conversation implied,
    # instructions live in the agent — no type/store/prompt knobs.
    kind: agent
    api:
        provider: openai
        # agent endpoints are complete base URLs; the SDK appends /responses
        base: https://my-account.services.ai.azure.com/api/projects/my-project/agents/my-agent/endpoint/protocols/openai
        model: gpt-5.4-mini
    auth:
        credentials: .env.agent
```

```dotenv
# .env.agent — the opt-in, nothing else
AZURE_USE_LOGIN=true
```

(To narrow which credential the underlying chain uses, additionally set
azidentity's own `AZURE_TOKEN_CREDENTIALS` selector in your shell — it
is a different variable with different values, which is why aibench's
opt-in is not spelled through it.)

## Gateways (APIM)

A gateway with its own key scheme takes it through the profile's
`api.headers` — name the variable for what the secret *is*, map it to
whatever header the gateway *wants*. This is the one method that
**composes**: a gateway that only meters takes its key *alongside* one
of the methods above in the same credentials file; a gateway that
authenticates to the backend itself (e.g. an
`authentication-managed-identity` policy) takes it alone, as here:

```yaml
apim:
    kind: model
    api:
        provider: openai
        base: https://my-gateway.azure-api.net/foundry/openai/v1
        model: gpt-5.4-mini
        headers:
            Ocp-Apim-Subscription-Key: ${SUBSCRIPTION_KEY}
    auth:
        credentials: .env.apim
```

```dotenv
# .env.apim — gateway key only; the gateway's policy authenticates
# to the backend, so no other method is set
SUBSCRIPTION_KEY=…
```

The route a gateway exposes, and how to read its failures, are in
[development/apim](development/apim.md).

APIM tip: an API can be configured with custom subscription-key names —
if the classic `Ocp-Apim-Subscription-Key` gets `401 missing subscription
key`, the API may take it as `api-key` (header) or `subscription-key`
(query) instead.

## When it fails

Auth is checked when a profile is **resolved** — at launch, on `/profiles`,
or on every `aibench send`. The CLI prints the error and exits; the TUI
shows it as a notice and leaves the previous profile active, so a broken
one never keeps the app off the screen.

**Two methods in one file.** The contract is exactly one, so the second
is named rather than silently ranked:

```console
$ aibench send -p dev "hello"
error: profile "dev": credentials conflict: OPENAI_API_KEY and AZURE_TOKEN
are all set — a profile authenticates one way. Keep the one this endpoint
wants and delete the others (api.headers is separate and always sent on top)
```

Check your shell too: an exported `AZURE_TOKEN` reaches the profile
through the fallback and conflicts with a key that is only in the file.

**No method at all.** The error names the file to edit, says what that
file already defines, and lists what you could set instead — a file
holding a secret no auth method reads is the most common way to land
here:

```console
$ aibench send -p dev "hello"
error: profile "dev": no credentials in .env.dev

  it defines MY_KEY, which no auth method reads and no api.headers entry references

  set one of these in it:
    OPENAI_API_KEY=…                                       an API key the endpoint issued (openai wire)
    ANTHROPIC_API_KEY=…                                    an API key the endpoint issued (anthropic wire)
    AZURE_TOKEN=…                                          a token you minted: az account get-access-token
    AZURE_USE_LOGIN=true                                   let aibench mint one from your az login
    AZURE_TENANT_ID / AZURE_CLIENT_ID / AZURE_CLIENT_SECRET a service principal

  or send MY_KEY as a header from the profile:
    api:
      headers:
        api-key: ${MY_KEY}
```

That last suggestion is the fix when the endpoint wants your secret in a
header of its own — a Foundry `api-key`, a gateway's subscription key —
rather than as one of the standard methods.

**A 401 at request time** means the credential resolved but the endpoint
refused it. In order of likelihood:

| Symptom | Usually |
| --- | --- |
| worked an hour ago, `AZURE_TOKEN` | the token expired — mint a fresh one, aibench never refreshes it |
| service principal or sign-in, never worked | the identity has no role on the resource (`Cognitive Services OpenAI User` or equivalent) |
| gateway in front | the subscription key's header name — see the [APIM tip](#gateways-apim) above |
| key auth on Foundry | the key is right but the header is not mapped: Foundry wants `api-key`, not `Authorization` |

The Inspector's **Request** view settles most of these in one look: it
lists the headers actually sent, so you can see *which* credential header
went out and under what name. Values are always redacted —
`authorization`, `api-key`, `x-api-key`, and any header your profile
declares — so it answers "was it sent, and as what", never "is the secret
correct".
