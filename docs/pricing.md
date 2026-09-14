# Pricing

Model rates behind the Metrics panel's `cost`, `session`, and `ctx` rows.
No pricing, no rows — everything else works without it.

## Schema

`pricing.yaml` maps a model id to its rates:

```yaml
gpt-5.4-mini:
    input: 0.25      # USD per 1M prompt tokens
    output: 2        # USD per 1M completion tokens
    context: 400000  # context window, drives the ctx row
```

## Where the rates come from

On a fresh install you have no pricing file, so the app fetches the
[table published from this repo](https://github.com/petrsx/aibench/tree/main/pricing)
once at launch and prices your sends with that. It gives up after a few
seconds if you are offline, and the cost rows simply stay empty.

The moment you have a `pricing.yaml` of your own, that file is the only
source and launch stops calling out. It is found the way the config is:
next to `aibench.yaml`, or in the folder you run from, or in
`~/.aibench/`; `PRICING_FILE` pins one explicitly.

## Filling your own

```console
$ aibench pricing update      # merge catwalk's registry into your file
$ aibench pricing edit        # open it and add a row by hand
```

`/pricing-update` inside the app does the same refresh, and says what
it changed in the notice line.

`update` is a merge: every model catwalk knows is added
or refreshed, and **rows only you have are kept**. That matters on Azure,
where a deployment name is yours alone and no registry will ever carry
it — set it once and it survives every update:

```yaml
my-gpt5-deployment:   # the deployment name, as the profile's model:
    input: 1.25
    output: 10
    context: 400000
```

`--out <file>` writes elsewhere; `--url <address>` (or `CATWALK_URL`)
reads from a different registry.

You don't have to restart for any of this. The file hot-reloads like the
config and prompt files do: save it, and the cost rows on screen reprice
right away — a record you sent a minute ago shows the new rate too.

## Refreshing what ships

The published table is a catwalk harvest committed to this repo. To
refresh it, harvest into a **clean** file so no private deployment-name
row can ride along, then commit:

```console
$ rm pricing/pricing.yaml
$ aibench pricing update --out pricing/pricing.yaml
```

Rate fixes and missing models are welcome as pull requests.
