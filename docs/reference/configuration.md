# Configuring the Tempo collector

Your activation file does double duty. The `host` and `process` sections configure the host, and everything under `config` reaches this collector. [config.example.yaml](../../config.example.yaml) gives you a complete file with placeholder values and no secrets, so copy it and make it yours.

One favor to ask before the tables: pick scope and instance IDs like you mean it. They're part of every stable fact key. If this activation replaces an older Tempo feed (in-tree or otherwise), copy the live values from that feed instead of inventing fresh ones, or Eshu will ingest the same feed twice under two identities. The values to keep equal: `host.scope.id` matches the old `scope_id`, `host.scope.kind` stays `trace_source`, `host.sourceSystem` stays `tempo`, and the activation instance ID matches the old collector instance ID. Run one collector per feed with a given identity, and don't ship the placeholders.

## Target settings

| Setting | How it behaves |
| --- | --- |
| `targets` | Required, with at least one target. |
| `targets[].provider` | Empty or `tempo`. Anything else fails closed, which catches copy-paste from sibling collectors. |
| `targets[].scope_id` | Required. The claim scope selects the target by this value. |
| `targets[].instance_id` | Required. Recorded as the fact source instance. |
| `targets[].base_url` | Required. The Tempo API origin. |
| `targets[].path_prefix` | Optional, for Tempo mounted under a sub-path. |
| `targets[].token_env` | Optional. Names the environment variable holding the token. Empty means unauthenticated Tempo, which is a legit setup. |
| `targets[].tenant_id` | Optional literal tenant ID. |
| `targets[].tenant_id_env` | Optional. Names the environment variable holding the tenant ID, and wins over `tenant_id` when both are set. |
| `targets[].resource_limit` | Bound on records read per target. The example uses `50`. |
| `targets[].tag_value_names` | Tags whose values get read. The example lists `resource.service.name`, a solid first pick. |
| `targets[].max_tag_values_per_tag` | Cap per tag, `20` in the example, so one busy tag can't eat the whole claim. |
| `targets[].stale_after` | Accepted and validated for parity with sibling collectors, but core owns freshness here and the producer ignores the value. Don't expect it to change behavior. |
| `targets[].enabled` | Disabled targets aren't served. |
| `targets[].declared_ids` | IDs you declare elsewhere, used as declared-vs-observed comparison hints for core. |
| `targets[].observed_only_hint` | Companion hint for that same core-owned comparison. |
| `targets[].freshness_probe_enabled` | Also call `GET /api/echo` as a freshness probe alongside the tag reads. |
| `targets[].lookback` | The tag-search window. The example uses `1h`. |

## Limits settings

Mind the casing in this repo, because it trips people up. The `limits` keys are camelCase here.

| Setting | How it behaves |
| --- | --- |
| `limits.maxResultBytes` | Above this many encoded bytes the claim fails terminal with `result_too_large`. The example sets `3145728`, the default is `3500000`, and the ceiling is 64 MiB. Keep `process.stdout_limit_bytes` above it. |
| `limits.maxRecordsPerClaim` | Accepted so the host envelope parses. The host enforces it, not this collector. |
| `limits.maxPayloadBytes` | Same story: parsed here, enforced by the host. |
| `limits.claimTimeoutSeconds` | Same story again. It documents the budget the host applies. |

Unknown config fields fail closed as terminal `invalid_config` before any provider contact. That's deliberate, and it includes `config.grant`, so don't try to smuggle grant material in here. A claim for a scope you haven't configured fails retryable with no facts.

## Credentials

Secrets travel by environment-variable name only. `token_env` and `tenant_id_env` name variables the collector process reads at claim time. The token is genuinely optional, since plenty of Tempo installs run unauthenticated, but naming a variable that resolves to nothing fails closed at config parse. Named-but-empty reads as a mistake, and we'd rather stop than guess.

When present, credentials ride in headers (`Authorization: Bearer` and `X-Scope-OrgID`), never in query parameters, facts, logs, or statuses. Give the credential only the read access the tag endpoints need. This repo doesn't name Tempo auth scopes or roles, because the code doesn't define any.

## Authorization, so you're not surprised

All three kinds are core-owned, and the host refuses them without a live producer grant (core's permission to emit a kind it owns). That enforcement happens host-side at emission. This collector carries no grant material, there's no `grant.example.json` in this repo, and the collect path runs no grant check of its own. If you're hunting for grant config keys here, save yourself the time. They live with the host.
