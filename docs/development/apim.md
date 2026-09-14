# Behind a gateway (APIM)

A gateway changes two things and nothing else: **the route** you point at,
and **the key** you send. Both are ordinary profile fields — there is no
gateway mode to turn on.

```yaml
profiles:
    via-apim:
        kind: model
        api:
            provider: openai
            base: https://my-gateway.azure-api.net/dev/openai/v1
            model: gpt-5.4-mini
            headers:
                Ocp-Apim-Subscription-Key: ${SUBSCRIPTION_KEY}
        auth:
            credentials: .env.apim
```

The key's header name is the gateway's choice, so it is written out rather
than inferred — see [authentication](../authentication.md#gateways-apim),
which also covers gateways that authenticate to the backend themselves.

## The route

Point `base:` at whatever the gateway exposes. Usually that is the **v1
surface** (`…/openai/v1`), where versioning is implicit and the SDK appends
the operation path — nothing else to declare.

A gateway fronting the **classic deployment route** needs two more things,
and both are strict:

```yaml
base: https://my-gateway.azure-api.net/dev/openai/deployments/{model}
query:
    api-version: 2024-06-01
```

- `{model}` is filled from `model:`, and reaches the gateway **verbatim** —
  dots included, so `gpt-5.4-mini` is sent as written.
- **`api-version` is required**, and its absence is a `404` rather than a
  message about a missing parameter. It rides `query:`, never the base URL.

That route is carried for compatibility; new work should point at the v1
surface. Either way it is only a base-URL spelling — aibench special-cases
neither.

## Reading a failure

| What you see | Usually means |
| --- | --- |
| `404` on every request | the base URL is not a route the gateway exposes — or, on the classic route, `api-version` is missing |
| `500 {"statusCode": 500}` for **every** deployment, including names that do not exist | the gateway→backend hop is broken — firewall or managed identity — not your request |
| `401 missing subscription key` | the gateway wants a different header name (`api-key`, or `subscription-key` as a query parameter) |
| a specific `4xx` | the backend answered; that one is about your request |

The last row is the useful distinction: **real backend errors come back
specific**, so anything uniformly generic is the hop in front of it.

The Inspector's Request view shows the URL, the query and the header names
actually sent (values redacted), which settles most of these in one look.
