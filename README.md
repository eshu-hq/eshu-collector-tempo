# eshu-collector-tempo

Status: pre-release. This collector is not yet the live Tempo producer for Eshu; Eshu core still owns admission and storage.

This collector reads live Tempo trace metadata for Eshu and returns it as facts (small, checked records Eshu stores as evidence).

## What it collects

For each work request, the collector reads one configured Tempo target through bounded REST calls and emits metadata only. It never stores spans or trace payloads.

`manifest.yaml` declares 3 fact kinds. Each kind uses schema version `1.0.0` and source confidence `reported` (the provider itself reported the value).

| Fact kind | What it records | Tempo read behind it |
| --- | --- | --- |
| `observability.source_instance` | One configured Tempo target | The configured target itself |
| `observability.observed_trace_signal` | Tag names, tag values, and freshness seen live | `GET /api/v2/search/tags`, `GET /api/v2/search/tag/{tag}/values`, optional `GET /api/echo` |
| `observability.coverage_warning` | A gap: permission-hidden, unsupported, throttled, or partial data | Any read above that could not finish cleanly |

Spans, trace retrieval payloads, raw trace IDs, TraceQL bodies, tag values in the clear, tenant IDs, credentials, and provider response bodies never enter a fact. Tag values are fingerprinted or flagged instead. There is no rules read and no truncation kind; a partial read is the only degraded shape.

The `/api/echo` freshness probe runs only when `freshness_probe_enabled` is set for the target. Tag and tag-value reads are the always-on path.

## How data flows

```mermaid
flowchart LR
    A[Tempo API] --> B[this collector]
    B --> C[Eshu extension host]
    C --> D[Eshu core]
```

Text version: Tempo API → this collector → Eshu extension host → Eshu core. The host hands the collector one claim (one bounded unit of work), validates the result, and owns claims, leases (timed work ownership), and commits. Core owns admission, projection, and persistence.

## When to use it

Use it when Eshu needs live evidence about which trace tags and services a Tempo target actually serves, and whether that data is fresh.

Do not use it to fetch traces for people, run TraceQL searches, or replace Tempo itself. It reads tag metadata only and writes nothing to Tempo.

## How Eshu runs it

The Eshu component-extension host runs this program with its `process` runner:

- The host writes one bounded `collector-sdk/v1alpha1` Request to standard input (stdin).
- The collector reads the configured Tempo targets and writes one `collector-sdk/v1alpha1` Result to standard output (stdout).
- With no arguments, or with `--sdk-stdio`, the binary serves one claim this way. That is the only serving mode.

Local smoke run with a recorded request:

```bash
go run ./cmd/collector < request.json > result.json
```

### Image and seed

The release image is `ghcr.io/eshu-hq/eshu-collector-tempo`. It holds one static binary at `/usr/local/bin/collector` on a distroless base (no shell), running as user `65532:65532`.

The binary installs itself for the host. An init container (a setup container that runs before the main one) runs:

```bash
collector seed --dest DIR [--image-ref ghcr.io/eshu-hq/eshu-collector-tempo@sha256:DIGEST]
```

Seed writes `DIR/collector` (mode 0755) and `DIR/manifest.yaml` (mode 0644). Re-running it is safe; files are replaced atomically.

`--image-ref`, or the `COLLECTOR_IMAGE_REF` environment variable (the flag wins), stamps the deployed digest into the seeded manifest, because an image cannot embed its own digest. One trailing newline is trimmed; any other shape exits non-zero before any file is written and never echoes the value. Without it, the repo placeholder (all-zero digest in `manifest.yaml`) is written unchanged — tests fail if a real digest is ever committed there.

Manifest identity: `dev.eshu.collector.tempo`, version `0.1.3`, collector kind `tempo`, platform `linux/amd64`, compatible core `>=0.0.5 <0.2.0`.

### Authorization

All 3 kinds are core-owned, so the host refuses them without a live producer grant (permission to emit a core-owned kind). Authorization is host-enforced at emission: grants arrive at the activation level, and this collector carries no grant material. There is no `grant.example.json` in this repo, and `config.grant` is rejected as an unknown field. The collect path performs no grant check of its own.

### Stable identity when replacing an older collector

Scope and instance IDs are part of every stable fact key. If this activation replaces an existing Tempo feed, copy these values from that feed's live configuration instead of inventing new ones, or Eshu will ingest the same feed as a new scope:

| Activation value | Must match the existing feed |
| --- | --- |
| `host.scope.id` | The existing target's `scope_id` |
| `host.scope.kind` | `trace_source` |
| `host.sourceSystem` | `tempo` |
| Activation instance ID | The existing collector instance ID |

Run only one collector per feed with the same scope and instance ID. Do not copy the placeholders in `config.example.yaml`.

## Configuration

The host reads an activation file. `host` and `process` configure the host; everything under `config` reaches this collector. `config.example.yaml` is a complete example with placeholder values and no secrets.

| Key | Meaning |
| --- | --- |
| `targets` | Required, at least one target. |
| `targets[].provider` | Must be empty or `tempo`; anything else fails closed. |
| `targets[].scope_id` | Required. The claim scope selects the target. |
| `targets[].instance_id` | Required. Recorded as the fact source instance. |
| `targets[].base_url` | Required. Tempo API origin. |
| `targets[].path_prefix` | Optional path prefix. |
| `targets[].token_env` | Optional. **Name** of the environment variable holding the token. Empty means unauthenticated Tempo. |
| `targets[].tenant_id` | Optional literal tenant ID. |
| `targets[].tenant_id_env` | Optional. **Name** of the environment variable holding the tenant ID; wins over `tenant_id`. |
| `targets[].resource_limit` | Bound on records read per target (example: `50`). |
| `targets[].tag_value_names` | Tags whose values are read (example: `resource.service.name`). |
| `targets[].max_tag_values_per_tag` | Cap per tag (example: `20`). |
| `targets[].stale_after` | Accepted and validated for parity, but core-owned; the producer ignores it. |
| `targets[].enabled` | Disabled targets are not served. |
| `targets[].declared_ids`, `targets[].observed_only_hint` | Hints for declared-vs-observed comparison, which core owns. |
| `targets[].freshness_probe_enabled` | Also call `GET /api/echo` as a freshness probe. |
| `targets[].lookback` | Tag-search window (example: `1h`). |
| `limits.maxResultBytes` | Fail terminal with `result_too_large` above this many encoded bytes (example: `3145728`; default `3500000`, ceiling 64 MiB). Note the camelCase `limits` keys in this repo. |
| `limits.maxRecordsPerClaim`, `limits.maxPayloadBytes`, `limits.claimTimeoutSeconds` | Accepted so the host envelope parses; the host enforces them. |

Unknown config fields fail closed as terminal `invalid_config` before any provider contact. An unconfigured claim scope fails retryable with no facts.

### Credentials

Secrets travel by environment-variable name only: `token_env` and `tenant_id_env` name variables the collector process reads at claim time. The token is optional — an empty `token_env` means unauthenticated Tempo — but a named-but-unset variable fails closed at config parse. Token and tenant travel in the `Authorization: Bearer` and `X-Scope-OrgID` headers, never in query parameters, facts, logs, or statuses.

Give the credential only the read access the tag endpoints above need. This repo does not name Tempo auth scopes or roles, because the code does not define them.

## Claim budget and operator signals

One claim has a hard deadline: the host kills the process at the initial lease expiry. The collector stops starting provider calls `min(2s, 5% of remaining time)` before that deadline. A claim that arrives with no time left fails retryable as `claim_deadline_exceeded` with no provider contact.

Failure mapping an operator will see:

| Provider outcome | Collector result |
| --- | --- |
| `401` / `403` | `permission_hidden` warning; result degrades to partial |
| `404` / `405` | `unsupported` warning |
| `429` | `rate_limited`; retryable provider failure |
| Other provider or transport failure | Terminal or retryable per the bounded failure class in the result |
| Bad activation config | Terminal `invalid_config`, before provider contact |
| Encoded result over `limits.maxResultBytes` | Terminal `result_too_large` |

The collector writes one JSON diagnostic line per claim to stderr for local runs and tests; the host process runner drops the text. The line carries claim identity and failure vocabulary only — never a token, URL, or provider body.

The manifest declares no telemetry section and this binary emits no metrics. Counts record against an internal seam and the claim result's failure class is the operator signal; the host adapts per-claim counters to its own signals.

## Eshu dependencies

Exact pins from `go.mod` in this repo. No `replace` directives.

| Module | Pin |
| --- | --- |
| `github.com/eshu-hq/eshu-sdk/collector` | `v0.2.0-rc.2` |
| `github.com/eshu-hq/eshu-sdk/factschema` | `v0.1.0-rc.12` |
| `gopkg.in/yaml.v3` | `v3.0.1` |

Go version in `go.mod`: `1.26.10`.

One known gap: the pinned `factschema` line referenced in older notes had no per-kind schema-version helper, so the locked `1.0.0` map lives in `tempo/schema.go`. Check that file before assuming the SDK now provides one.

## What it never does

- It never writes to Tempo and never persists spans or traces.
- It has no Postgres, graph, queue, or lease handle, and never imports Eshu core internals (`boundary_test.go` enforces this).
- It never builds the durable fact envelope or commits facts; the host does.
- It carries no grant material and never authorizes itself.
- It never emits kinds outside the 3 in `manifest.yaml`.

## Build, test, and release

```bash
make ci          # lint + test + race
make lint        # vet, golangci-lint, format diff, tidy diff, 500-line cap
make test        # go build ./... and go test ./... -count=1
make race        # race tests plus coverage floor (90%)
make actionlint  # workflow lint
docker build -t eshu-collector-tempo:local .
```

CI (`.github/workflows/ci.yml`) is the authority; `make ci` mirrors it locally.

Releases run from `.github/workflows/release.yml` on pushes to `main` and on version tags. Nothing publishes from a pull request and the workflow deploys nothing. A `main` push publishes moving `main`, `latest`, and `sha-<full-sha>` tags; a version tag must equal `v` plus `metadata.version` in `manifest.yaml` (a pre-release suffix is allowed) and point at the protected `main` head. The workflow builds `linux/amd64`, pushes `ghcr.io/eshu-hq/eshu-collector-tempo:<tag>`, signs the digest with cosign (keyless), and attaches SLSA provenance and an SPDX SBOM attestation (signed build records). It needs no secret beyond the job's `GITHUB_TOKEN`. Copy the digest reference from the job summary into the activation.

## Docs and related repos

- [eshu](https://github.com/eshu-hq/eshu) — Eshu core and the extension host that runs this collector.
- [eshu-sdk](https://github.com/eshu-hq/eshu-sdk) — the collector and fact-schema modules pinned above.
- [eshu-deploy](https://github.com/eshu-hq/eshu-deploy) — deployment for Eshu components.

Local files: [config.example.yaml](config.example.yaml), [manifest.yaml](manifest.yaml), [Dockerfile](Dockerfile), and [CONTRIBUTING.md](CONTRIBUTING.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). This repo has no `SECURITY.md` and no `LICENSE` file; source files carry an SPDX MIT header.
