// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tempo collects bounded live Tempo metadata as observability source
// facts for the collector-sdk/v1alpha1 wire contract.
//
// The package reads configured Tempo API targets and emits metadata-only
// observability.source_instance, observability.observed_trace_signal, and
// observability.coverage_warning facts. It never persists spans, trace
// retrieval payloads, raw trace IDs, TraceQL bodies, tag values, tenant IDs,
// tenant headers, credentials, or provider response bodies. Core owns
// admission, projection, persistence, claim mutation, fencing, and
// declared/applied/observed comparison: this package returns a
// collector.Result per host claim. Authorization is host-enforced at
// emission; this package carries no grant material.
//
// Extracted from eshu-hq/eshu go/internal/collector/observability/tempo
// (#8867). Contract payloads merge through the pinned
// github.com/eshu-hq/eshu-sdk/factschema module; HTTP transport and failure
// classification come from the pinned
// github.com/eshu-hq/eshu-sdk/collector/transport package.
package tempo
