# Facts reference

## The facts, all three of them

This list mirrors `manifest.yaml` and its `emittedFacts`. Treat the two as a pair: change one, update the other. Each kind uses schema version `1.0.0` and source confidence `reported`, meaning Tempo reported the value itself.

| Kind | Reading one tells you… | Tempo read behind it |
| --- | --- | --- |
| `observability.source_instance` | this Tempo target is configured | the target from your config |
| `observability.observed_trace_signal` | these tag names and values are live, with freshness attached | `GET /api/v2/search/tags`, `GET /api/v2/search/tag/{tag}/values`, optional `GET /api/echo` |
| `observability.coverage_warning` | part of the picture is missing or degraded | whichever read above hit trouble |

There's no rules read and no truncation kind here, unlike some siblings. A partial read is the only degraded shape you'll meet. The `/api/echo` probe only runs when `freshness_probe_enabled` is set for the target. Tag and tag-value reads run every time.

What never crosses into a fact: spans, trace payloads, raw trace IDs, TraceQL bodies, tag values in the clear, tenant IDs, credentials, and provider response bodies. If a value matters for matching, you get its fingerprint. If it doesn't, you get a presence flag.

## SDK pins

Copied from `go.mod`, which wins any disagreement. No `replace` directives.

| Module | Pin |
| --- | --- |
| `github.com/eshu-hq/eshu-sdk/collector` | `v0.2.0-rc.2` |
| `github.com/eshu-hq/eshu-sdk/factschema` | `v0.1.0-rc.12` |
| `gopkg.in/yaml.v3` | `v3.0.1` |

Go `1.26.11`, per `go.mod`. One leftover gap: older notes flagged that the pinned `factschema` line had no per-kind schema-version helper, so the locked `1.0.0` map lives in `tempo/schema.go`. Check that file before you assume the SDK provides one now.

The manifest ([manifest.yaml](../../manifest.yaml)) stays the source of truth for this list. If a fact kind ever changes, the manifest and this page change together.
