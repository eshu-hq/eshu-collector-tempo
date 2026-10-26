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
- `grant/` — fail-closed stub enforcing the core-issued producer grant
  draft (`producer-grant.manifest.DRAFT.yaml` in eshu-hq/eshu,
  `go/cmd/collector-tempo`) at every emission. Until core issues a
  grant, the zero Grant is denied and `Collect` fails closed before any
  provider contact.

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
- Grant `MaxLifetime` (720h) follows the draft's issuance example;
  expiry/rotation ownership is recorded at cutover, not here.

## Relation to eshu-collector-grafana (#8857) and eshu-collector-loki (#8860)

This repository mirrors that layout file for file (`doc.go`, `types.go`,
`envelope.go`, `source.go`, `client.go`, `redaction.go`, `metrics.go`,
`schema.go`, `claimid.go`, `grant/`, CI). Deliberate Tempo deltas:

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
- No Dockerfile, chart, or collector main: the extracted producer is a
  library the host embeds, same as the Grafana and Loki producers.
  Packaging and deployment stay host-owned.
