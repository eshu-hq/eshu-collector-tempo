// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package collector is the SDK adapter for the extracted Tempo trace-signal
// producer (eshu-hq/eshu#9172, extraction eshu-hq/eshu#8867). It turns one
// collector-sdk/v1alpha1 host request into one SDK result: it validates the
// request, resolves the activation config (targets plus the host-delivered
// producer grant) against the process environment, runs the tempo producer,
// and maps every outcome — including a denied grant or an unconfigured scope
// — to a result the host validator accepts. It never mints a grant:
// production grants are issued by core, and without one emission fails
// closed before any provider contact.
package collector
