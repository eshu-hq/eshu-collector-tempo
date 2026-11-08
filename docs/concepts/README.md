# Concepts

## What this is

A pre-release collector that asks a live Tempo target which trace tags exist and what values show up for the few tags you name, then reports the answers to Eshu as facts, with freshness attached. Spans and trace payloads stay in Tempo where they belong.

## Why it exists

Eshu needs live evidence about trace coverage — which services a Tempo target has actually seen lately — without dragging bulky, sensitive traces across the wire.

## The one idea to keep: tags in, fingerprints out

Tag values are fingerprinted or flagged, never stored in the clear. Raw trace IDs, TraceQL bodies, tenant IDs, credentials, and provider response bodies never cross into a fact either.

## Claims and grants

The host hands over one claim (one bounded unit of work) at a time and owns claims, leases, and commits; core owns admission and storage. All three emitted kinds are core-owned, so the host enforces a live producer grant at emission — this collector carries no grant material of its own (see [Configuration](../reference/configuration.md)).

## Boundaries

It never writes to Tempo or persists traces, holds no Postgres, graph, queue, or lease handle, and never imports Eshu core internals. There is no rules read here, unlike some siblings: tag and tag-value reads run every claim, and the `/api/echo` freshness probe runs only when you enable it.
