# Contributing to eshu-collector-tempo

Welcome — thanks for helping. The short version: run the local checks before you push, keep credentials out of code and logs, and never weaken a test to make a change land.

```bash
make ci          # lint + test + race
make lint        # vet, golangci-lint, format diff, tidy diff, 500-line cap
make test        # go build ./... and go test ./... -count=1
make race        # race tests plus the coverage floor (90%)
make actionlint  # workflow lint
docker build -t eshu-collector-tempo:local .
```

The root [CONTRIBUTING.md](../../CONTRIBUTING.md) stays the authority for CI lanes and floors. This page mirrors it so the docs tree is self-contained; if the two ever disagree, believe the root file and fix this page.

The test suite is the product contract: the parity, golden, hostile-input,
and boundary tests must stay green. Never weaken them to land a change.

Credentials travel by env reference, never inline. The handler resolves
token and tenant variables through its env seam; tests inject a stub.
Never log a token, tenant ID, URL, or provider body.

## Local checks

You need Go (see `go.mod`), `golangci-lint` v2.12.2, and Docker for the
image lane.

```sh
make ci   # lint + test + race, the whole local contract
```

`make lint` runs `go vet`, whole-repo `golangci-lint run` (config in
`.golangci.yml`, org lint set; no baselines), the gofumpt format check,
`go mod tidy -diff`, and the 500-line cap on `.go` files. `make race`
also enforces the statement-coverage floor (`COVERAGE_FLOOR`, at 90%,
the epic finish line; it may only rise). `make actionlint` mirrors
the actionlint lane.

## CI lanes

`.github/workflows/ci.yml` is a thin caller over the shared
`go-collector-ci` reusable (pinned by SHA): it runs a `changes` job
first, so a PR only pays for lanes its paths can affect, then calls
`go-ci` with this repo's run-gate wiring. Go sources select `lint`,
`test`, `race`, `image` (the Dockerfile builds the binary from them),
`boundary`, and `gosec`; fixtures and embedded config select `test`;
`Dockerfile` and manifest changes select `image`; dependency changes
select `govulncheck`; workflow changes select `actionlint` plus the Go
lanes. Check runs nest as `go-ci / <lane>`, so `go-ci / ci-complete`
is the only required check: it fails when a needed lane fails and
passes when lanes were legitimately skipped (docs-only PRs).

The two jobs the reusable cannot own stay in the caller:
`scheduled-findings` files a single weekly issue when the scheduled
`govulncheck`/`gosec` lanes fail, and `gosec-sarif` uploads SARIF to
the Security tab (advisory until its false-positive rate is measured).
`scripts/tempo-seed-matrix.sh` reseeds only the locally provable lanes;
the upstream lane bodies are proven by the reusable's own selftest.
Dependabot proposes weekly grouped `gomod` and `github-actions` bumps.
