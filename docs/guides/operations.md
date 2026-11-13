# Operating the Tempo collector

Most of the time you'll interact with this collector through the host, not directly. Still, it's worth knowing how it installs, what its results are telling you, and where to poke when a target goes quiet.

## How it runs

The extension host uses its `process` runner: one `collector-sdk/v1alpha1` Request in on stdin, one Result out on stdout. No arguments (or `--sdk-stdio`) serves one claim, and that's the only serving mode. If you're looking for flags, ports, or a health endpoint, there aren't any.

```bash
go run ./cmd/collector < request.json > result.json
```

## Image and seed

Releases publish `ghcr.io/eshu-hq/eshu-collector-tempo`, a single static binary at `/usr/local/bin/collector` on a distroless base with no shell, running as user `65532:65532`. The binary doubles as its own installer. Your init container runs something like:

```bash
collector seed --dest DIR [--image-ref ghcr.io/eshu-hq/eshu-collector-tempo@sha256:DIGEST]
```

Seed writes `DIR/collector` (mode 0755) and `DIR/manifest.yaml` (mode 0644), replacing files atomically. Re-run it whenever you like. It's built for that.

Pass `--image-ref`, or set `COLLECTOR_IMAGE_REF` and let the flag win when both exist, and seed stamps the deployed digest into the seeded manifest. An image can't know its own digest, so the deployer supplies it. One trailing newline gets trimmed, because copy-paste happens. Any other shape exits non-zero before a single file is written, and the bad value never gets echoed back. Skip the ref and the repo placeholder (an all-zero digest in `manifest.yaml`) ships unchanged. Tests fail loudly if a real digest ever gets committed there, which keeps everyone honest.

Identity for your notes: `dev.eshu.collector.tempo`, version `0.1.3`, collector kind `tempo`, platform `linux/amd64`, compatible core `>=0.0.5 <0.2.0`.

## Deadlines

One claim, one hard deadline. The host kills the process at the initial lease expiry, and the collector stops starting provider calls `min(2s, 5% of remaining time)` before that line. A claim that shows up already out of time fails retryable as `claim_deadline_exceeded` with zero provider contact. If you see that class often, either the lease is tight or the target is slow, and the lease is the easier fix.

## Reading the signals

Provider outcomes map to results like this. Keep the list near your dashboards:

- `401` or `403` from Tempo becomes a `permission_hidden` warning, and the result degrades to partial. You still get what the credential could see.
- `404` or `405` becomes an `unsupported` warning. That usually means the endpoint or API version differs from what this collector expects.
- `429` becomes `rate_limited`, a retryable provider failure. Back off and let the host retry.
- Other provider or transport trouble lands as terminal or retryable per the bounded failure class in the result.
- A bad activation fails terminal as `invalid_config` before provider contact.
- An oversized result fails terminal as `result_too_large` against `limits.maxResultBytes`.

Each claim also writes one JSON diagnostic line to stderr. It's genuinely useful locally and in tests, and it carries claim identity and failure vocabulary only, never a token, a URL, or a provider body. The host's process runner drops the text in production, so treat it as a debugging aid, not a log stream.

Metrics, plainly: the manifest declares no telemetry section and this binary emits no metrics. Counters record against an internal seam, the claim result's failure class is your operator signal, and the host adapts per-claim counters into its own signals if it wants them.

## Troubleshooting, in the order we'd try it

Start with config. `invalid_config` almost always means a stray key or a casing slip in `limits`, so compare against the example. Remember that unknown fields fail closed on purpose.

Next, check the credential story. A named-but-unset `token_env` fails at parse time, while `permission_hidden` warnings mean the token resolved fine but can't see everything. Those are different problems with different fixes.

If results come back partial with `unsupported` warnings, confirm the target really serves `/api/v2/search/tags`. An older Tempo or a proxy rewriting paths will show up exactly like this. And if tag values look thin, check `tag_value_names`, `max_tag_values_per_tag`, and `lookback` before blaming Tempo. A one-hour window on a quiet service can legitimately return very little.

Stuck after that? Run the binary locally with the activation's config and a recorded request, then read the result JSON next to the stderr line. That pair answers most questions.

## Build, test, and release

```bash
make ci          # lint + test + race
make lint        # vet, golangci-lint, format diff, tidy diff, 500-line cap
make test        # go build ./... and go test ./... -count=1
make race        # race tests plus the coverage floor (90%)
make actionlint  # workflow lint
docker build -t eshu-collector-tempo:local .
```

CI in `.github/workflows/ci.yml` is the authority. `make ci` mirrors it closely enough for local work.

Releases flow from `.github/workflows/release.yml` on `main` pushes and version tags. Pull requests publish nothing, and the workflow deploys nothing. A `main` push gets moving `main`, `latest`, and `sha-<full-sha>` tags. A version tag must equal `v` plus `metadata.version` from `manifest.yaml` (a pre-release suffix is fine) and point at the protected `main` head. The job builds `linux/amd64`, pushes to GHCR, signs the digest with keyless cosign, and attaches SLSA provenance and an SPDX SBOM attestation. It needs no secret beyond the job's `GITHUB_TOKEN`. Grab the digest reference from the job summary for your activation.

## SDK pins

Copied from `go.mod`, which wins any disagreement. No `replace` directives.

| Module | Pin |
| --- | --- |
| `github.com/eshu-hq/eshu-sdk/collector` | `v0.2.0-rc.2` |
| `github.com/eshu-hq/eshu-sdk/factschema` | `v0.1.0-rc.12` |
| `gopkg.in/yaml.v3` | `v3.0.1` |

Go `1.26.11`, per `go.mod`. One leftover gap: older notes flagged that the pinned `factschema` line had no per-kind schema-version helper, so the locked `1.0.0` map lives in `tempo/schema.go`. Check that file before you assume the SDK provides one now.
