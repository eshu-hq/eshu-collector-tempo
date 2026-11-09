# Getting started

The extension host normally runs this binary. Getting started is a smoke run, a seed, and an activation.

## 1. Smoke-test it

```bash
go run ./cmd/collector < request.json > result.json
```

One request in on stdin, one result out on stdout. That is the only serving mode.

## 2. Seed the release image

Releases publish `ghcr.io/eshu-hq/eshu-collector-tempo`. Your init container runs:

```bash
collector seed --dest DIR [--image-ref ghcr.io/eshu-hq/eshu-collector-tempo@sha256:DIGEST]
```

Seed writes `DIR/collector` and `DIR/manifest.yaml`, safe to re-run. The details live in the [operations guide](../guides/operations.md).

## 3. Write the activation

Start from [config.example.yaml](../../config.example.yaml). Keep `host.sourceSystem: tempo` and `host.scope.kind: trace_source`, name at least one target, and pick the tags whose values you want read (`resource.service.name` is a solid first pick). Secrets travel by environment-variable name only. Every key is in [Configuration](../reference/configuration.md) — mind the camelCase `limits` keys in this repo.

One favor: choose scope and instance IDs like you mean it. They sit inside every stable fact key, so fresh values make the same Tempo feed look brand-new.

## Where next

- How tags become facts: [Architecture](../architecture/README.md).
- Signals and troubleshooting: the [operations guide](../guides/operations.md).
