# Architecture

Traces are bulky and sensitive, so this collector takes the light-touch route. It asks Tempo which tags exist and what values show up for a few tags you name, then fingerprints the interesting bits. You learn the shape of trace coverage without dragging spans across the wire.

## Where it sits

See [Diagrams](../diagrams/README.md) for the picture.

Text version: Tempo API, then this collector, then the extension host, then Eshu core. The host hands over one claim, validates whatever comes back, and owns claims, leases, and commits. Core owns admission, projection, and persistence. This collector owns the conversation with Tempo and nothing downstream of it.

## Following one claim through

See [Diagrams](../diagrams/README.md) for the picture.

In plain steps: a request arrives, config gets checked before any network call, and the claim scope selects a target. The collector reads tag names, then values for the tags you listed, and optionally pings `/api/echo` as a freshness probe when you've enabled it. Tag values get fingerprinted or flagged rather than stored in the clear, and the result heads back to the host.

The deadline guard keeps this honest. The collector stops starting provider calls `min(2s, 5% of remaining time)` before the host's kill line. A claim that arrives with no time left fails retryable as `claim_deadline_exceeded` without touching Tempo at all.

## The facts, all three of them

This list mirrors `manifest.yaml` and its `emittedFacts`. Treat the two as a pair: change one, update the other. Each kind uses schema version `1.0.0` and source confidence `reported`, meaning Tempo reported the value itself.

| Kind | Reading one tells you… | Tempo read behind it |
| --- | --- | --- |
| `observability.source_instance` | this Tempo target is configured | the target from your config |
| `observability.observed_trace_signal` | these tag names and values are live, with freshness attached | `GET /api/v2/search/tags`, `GET /api/v2/search/tag/{tag}/values`, optional `GET /api/echo` |
| `observability.coverage_warning` | part of the picture is missing or degraded | whichever read above hit trouble |

There's no rules read and no truncation kind here, unlike some siblings. A partial read is the only degraded shape you'll meet. The `/api/echo` probe only runs when `freshness_probe_enabled` is set for the target. Tag and tag-value reads run every time.

What never crosses into a fact: spans, trace payloads, raw trace IDs, TraceQL bodies, tag values in the clear, tenant IDs, credentials, and provider response bodies. If a value matters for matching, you get its fingerprint. If it doesn't, you get a presence flag.

## What it won't do for you

It won't write to Tempo or persist traces. It holds no Postgres, graph, queue, or lease handle, and it never imports Eshu core internals, with `boundary_test.go` standing guard. It won't build the durable envelope or commit facts, since that's host work. It also carries no grant material and never authorizes itself. Grants arrive at the activation level and the host enforces them at emission.
