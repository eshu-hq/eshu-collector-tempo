// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package collector is the SDK adapter for the extracted Tempo trace-signal
// producer (eshu-hq/eshu#9172, extraction eshu-hq/eshu#8867). It turns one
// collector-sdk/v1alpha1 host request into one SDK result: it validates the
// request, resolves the activation config (targets plus the shared limits
// stanza) against the process environment, runs the tempo producer, and maps
// every outcome — including a bad activation or an unconfigured scope — to a
// result the host validator accepts. Authorization is host-enforced at
// emission; the satellite carries no grant material.
package collector
