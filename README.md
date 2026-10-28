# eshu-collector-tempo

Extracted Tempo trace-signal collector producer for [eshu-hq/eshu#8867](https://github.com/eshu-hq/eshu/issues/8867)
(collector-tempo extraction, part of epic #6707). Emission half only:
this repository fetches bounded live Tempo metadata and returns a
`collector-sdk/v1alpha1` Result per host claim. Core keeps admission,
projection, persistence, claim mutation, fencing, and the status read.

Refs #8867. This repository does not close that issue: cutover stays gated
by #4047 with production acceptance NOT_CHECKED, and no pair is a
qualified consumer/core combination yet (see SDK Compatibility in
eshu-hq/eshu docs).

## Pins

Exact published pins from `eshu-hq/eshu-sdk` — never `@latest`, no
`replace` or workspace directives:

| Module | Pin |
|---|---|
| `github.com/eshu-hq/eshu-sdk/collector` | `v0.1.0-rc.3` |
| `github.com/eshu-hq/eshu-sdk/factschema` | `v0.1.0-rc.5` |

Verify resolution before any pin bump:

```bash
GOPROXY=proxy.golang.org go list -m github.com/eshu-hq/eshu-sdk/collector@v0.1.0-rc.3
GOPROXY=proxy.golang.org go list -m github.com/eshu-hq/eshu-sdk/factschema@v0.1.0-rc.5
```

## Layout

- `tempo/` — the producer: bounded REST fetch (`client.go`,
  `normalize.go`), three envelope constructors (`envelope.go`),
  claim-to-Result fan-out (`source.go`), metadata-only redaction
  (`redaction.go`), kind/schema admission (`schema.go`), claim-ID minting
  (`claimid.go`), and a host-wired metrics seam (`metrics.go`).
  `testdata/8867-parity-tempo.json` is the cutover comparison input,
  byte-identical to the in-tree parity pack.
- `contract.go`, `request.go`, `config.go`, `handler.go`, `failure.go` —
  the SDK adapter: the emitted contract, the stdio request wire shape,
  activation parsing (targets plus the shared host-level limits stanza),
  and the claim-to-Result handler that maps every outcome to a
  validator-clean result.
- `cmd/collector/` — the standalone entrypoint: one claim from stdin by
  default (`--sdk-stdio`), and `seed` to install the binary and manifest
  into a component home from an init container.
- `manifest.yaml` (`manifest_embed.go`) — the component package at version
  `0.1.3`, with digest-pinned placeholder artifacts and the three emitted
  kinds (`observability.source_instance`,
  `observability.observed_trace_signal`, `observability.coverage_warning`)
  at `1.0.0` reported.
- `Dockerfile` — the static release image the tag-gated workflow publishes.
- `config.example.yaml` — a starting activation config (placeholders, no
  secrets).
- `.github/workflows/` — `ci.yml` (vet, tests with `-race`, import
  boundary, image seed contract) and `release.yml` (tag-gated image
  publish with signature and attestations).

## Parity evidence

- Census mirror: `tempo/extraction_prep_mirror_test.go` ports the
  in-tree extraction PREP mirror (admission at 1.0.0, determinism,
  generation rebind, tag-value hash flow-through, redaction sentinel,
  fail-closed hostile inputs). `TestClaimIDNamespace` ports the
  `tempo-claim-` namespace case.
- Golden vectors: `tempo/extraction_prep_golden_test.go` pins the three
  in-tree stable keys, the tag-values and source-instance payload bytes
  cut from the in-tree constructors at the merged prep (eshu-hq/eshu
  `a91b8dd8`) against the stub-replayed parity run, plus a live
  httptest run that reproduces the recorded pack's stable keys
  (permission-hidden degradation stays covered by the in-tree parity
  test), a per-kind census, and a validator self-check. Regenerate only
  from the in-tree constructors, never from this package's own output.
- Stable keys: `kind:generationID:identity.StableID(kind, identity)` is
  byte-stable with the in-tree scheme (the published
  `factschema/identity` algorithm is identical to in-tree
  `facts.StableID`; the golden test proves it empirically).
- Contract payloads: `observability/v1` struct fields are identical
  between the in-tree `sdk/go/factschema` source and the pinned module,
  and the same `EncodeObservability*` entry points merge payloads.
- SDK self-check: `TestCollectEmitsValidatorCleanResult` and
  `TestCollectPartialReadStaysPartial` run emitted Results through the
  published SDK's own `collector.NewValidator(...).ValidateResult`,
  including its sensitive-payload-key gate.
- In-tree `FactID` (host-assigned at admission) and numeric
  `FencingToken` (host-enforced from `claim.fencing_token`) have no
  emission-half counterpart and are intentionally not ported.
  `source_ref.uri` carries the target base URL because the SDK Fact wire
  shape requires it; the in-tree envelope has no URI counterpart.

## Boundaries (core-owned, not ported)

Instance selection, `token_env`/`tenant_id_env` resolution, the
heartbeat-under-lease guard, the Postgres commit boundary, cassette
replay, and `eshu_dp_tempo_*` signals stay in core. The
`tempo.observe`/`tempo.fetch` spans stay core-traced; the host observes
the `Collect` call itself. Telemetry records against the local
`MetricsRecorder` seam; the host adapts it to its own signals.
`source_system` binding is strict (`tempo` exactly): the SDK host
validator requires fact source_system to equal the claim's, so anything
else fails closed at the edge.

Credential transport: the token is OPTIONAL (empty `token_env` means
unauthenticated Tempo; a named-but-unset variable fails closed at the
core-owned config layer). Token and tenant travel in the
`Authorization: Bearer` and `X-Scope-OrgID` headers, never in query
parameters, facts, logs, metric labels, or docs. TraceQL, tag values,
and tenant IDs are fingerprint-or-flag redacted before emission.

Failure classification: 403/401 append a `permission_hidden` warning and
degrade to partial; 404/405 append `unsupported`; 429 appends
`rate_limited` and returns a retryable provider error; anything else
returns a terminal provider error. Unknown claim scope IDs fail as
retryable provider failures. Two in-tree config knobs have no producer
counterpart: the per-target `Now` clock hook is dropped (test-only
narrowing; production-identical when nil) and `StaleAfter` is dropped
from `TargetConfig` (core-config-parsed, producer-unread — correctly
core-owned).

## Deferred to the host

- Claim acquisition wiring for `newClaimID` (the in-tree
  `service.go` equivalent).
- `Lookback` default: the 1h fallback bounds the tag-search window;
  narrow it only when the deployment's retention window is shorter.

## Open gaps

- P1: the published factschema `v0.1.0-rc.5` has no per-kind
  schema-version map or `ValidateSchemaVersion` admission helper
  (compare `ObservabilitySchemaVersion` / `ValidateSchemaVersion` in
  eshu-hq/eshu `go/internal/facts`). The locked `1.0.0` map lives in
  `tempo/schema.go` until the SDK gains one.

## Relation to eshu-collector-grafana (#8857) and eshu-collector-loki (#8860)

This repository mirrors that layout file for file (`doc.go`, `types.go`,
`envelope.go`, `source.go`, `client.go`, `redaction.go`, `metrics.go`,
`schema.go`, `claimid.go`, CI). Deliberate Tempo deltas:

- `normalize.go`: tag-set/tag-value normalization split out of
  `client.go` so both files stay under the 500-line cap (same split as
  the in-tree `http_normalize.go`).
- `testdata/8867-parity-tempo.json`: the #4047 cutover comparison input
  travels with the repo (byte-identical to in-tree).
- No rules path and no truncation concept: three kinds only, and a
  partial read is the only degraded-completeness shape.
- Freshness probe (`/api/echo`) runs only when the host enables it; tag
  and tag-value reads are the always-on path.
- Token is optional (unauthenticated Tempo is a supported target); Loki
  and Grafana treatments of their own credentials are unchanged.
- Standalone surface added for the QA cutover (#9172): `cmd/collector/`,
  `Dockerfile`, `manifest.yaml`, and the tag-gated `release.yml`, following
  the same shape as the Grafana, Loki, and security-alerts satellites. No
  chart: deployment stays GitOps-owned in `boatsgroup/iac-eks-eshu`.

## Running

The collector runs through the Eshu component-extension host with the
`process` runner. The host writes one bounded `collector-sdk/v1alpha1`
request to stdin and reads one result from stdout. The collector gets no
Postgres, graph, queue, lease, or workflow handle.

```bash
go run ./cmd/collector < request.json > result.json
```

A bad activation fails closed as terminal `invalid_config` before any
provider contact; an unconfigured scope fails retryable with no facts. An
oversized result fails terminal as `result_too_large` when the encoded result
exceeds `config.limits.maxResultBytes`. A claim arriving with no lease time
left fails retryable as `claim_deadline_exceeded`. The token stays optional:
an empty `token_env` means unauthenticated Tempo, while a named-but-unset
variable fails closed at config parse. The host passes the activation config
block verbatim, including the shared `limits` stanza and the per-target
`provider` key; `provider` must be empty or `tempo`, and the satellite
enforces `limits.maxResultBytes` against its encoded result.

The collector writes one JSON diagnostic line per claim to stderr for local
runs and tests; the host's process runner drops the text. The line carries
claim identity and the failure vocabulary only: never a token, URL, or
provider body.

## Image and seeding

The image holds one static binary that installs itself, so an init container
needs no shell:

```bash
collector seed --dest /component [--image-ref ghcr.io/eshu-hq/eshu-collector-tempo@sha256:<digest>]
```

writes `/component/collector` (mode 0755) and `/component/manifest.yaml`.
`--image-ref REF` (or the `COLLECTOR_IMAGE_REF` environment variable; the flag
wins) stamps the deployed digest-pinned reference into the manifest's artifact
entry, because an image cannot embed its own digest. The repository manifest
keeps the all-zero placeholder digest permanently (`manifest_test.go` fails on
a real one), because it is embedded in the binary and a committed digest could
only describe an older build.

## Cutover identity

Replacing the in-tree Tempo collector with this one must not re-key a single
fact. These activation values have to equal what the in-tree collector uses
for the same feed:

| Activation value | Must equal the in-tree value | If it differs |
| --- | --- | --- |
| `host.scope.id` | the in-tree target's `scope_id` | every stable key changes: the feed is ingested as a new scope |
| `host.scope.kind` | `trace_source` | the scope no longer matches the in-tree scope kind |
| `host.sourceSystem` | the in-tree source system, `tempo` | `source_ref` changes on every fact |
| activation instance id | the in-tree collector instance id | `collector_instance_id` in payloads changes |

The collector copies `scope_id`, `collector_instance_id`, and `source_system`
from the claim the host sends; it has no setting of its own for them.

Before enabling an activation, read the values from the live in-tree
collector instance configuration and write them into the activation. Do not
copy the placeholders in `config.example.yaml`. Run only one of the two
collectors per feed: with the same scope and instance id they would write the
same stable keys.

## Authorization

The emitted kinds are core-owned, so the host refuses them without a live
producer grant. Authorization stays host-enforced at emission: the activation
carries `grants` at the activation level (issued again on every pod start by
the reconcile init containers), and the satellite carries no grant material.
`config.grant` is rejected as an unknown field, and the collect path performs
no grant check.

The manifest declares the full three-kind emission set at `1.0.0`
reported, and the grant keeps all three covered kinds: the host classifies
emission by grant, not by manifest. Live QA re-proof against a real Tempo
target is pending (NOT_CHECKED).

## Releasing

`.github/workflows/release.yml` runs only when a tag `vMAJOR.MINOR.PATCH[-pre]`
is pushed. Nothing runs on a pull request or a branch push, and the workflow
deploys nothing.

The workflow refuses to publish unless the tag points at the protected
`main` head, so a `v*` tag cut from any other commit publishes nothing. As
defense in depth, restrict who can create `v*` tags with a tag ruleset
limited to maintainers.

1. Set `metadata.version` in `manifest.yaml` to the version you are releasing
   and merge that change to `main`. Then tag the merge commit and push the tag.
   The workflow refuses to publish unless the tag equals `v` plus that version
   (a `-pre-release` suffix on the tag is allowed when the manifest version has
   none), and it checks this before it builds or pushes anything.
2. The workflow builds `linux/amd64` (the only platform the manifest declares),
   pushes `ghcr.io/eshu-hq/eshu-collector-tempo:<tag>`, writes the
   digest to the job summary, signs the digest with cosign (keyless), and
   attaches a SLSA build-provenance attestation and an SPDX SBOM attestation.
   It needs no secret beyond the job's `GITHUB_TOKEN`.
3. Copy the `ghcr.io/eshu-hq/eshu-collector-tempo@sha256:<digest>`
   reference from the job summary into the activation's `seed.image`. The
   deployment stamps it into the manifest with `collector seed --image-ref`.

## Build and test

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
```

`go test -count=1 -run TestImportBoundaryRejectsCoreAndCrossCollectorImports .`
is the standalone gate: no Eshu internal imports and no cross-collector
imports beyond the published SDK modules.

`make ci` runs the lint, test, and race lanes CI runs
(`.github/workflows/ci.yml`; the `ci-complete` job aggregates the others).

Fork PRs run the same lanes on the automatic read-only token: secret-requiring
steps (SARIF upload, findings issues) are skipped or schedule-gated.
