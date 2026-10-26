// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/transport"

	"github.com/eshu-hq/eshu-collector-tempo/grant"
	"github.com/eshu-hq/eshu-collector-tempo/tempo"
)

// Handler runs one claim. The zero value is usable: it reads the process
// environment, the wall clock, and the production Tempo HTTP client.
type Handler struct {
	// Getenv resolves credential environment variables; nil selects an
	// empty environment so missing credentials fail closed.
	Getenv func(string) string
	// Now supplies the clock; nil selects time.Now.
	Now func() time.Time
	// ClientFactory builds provider clients; nil selects the production
	// Tempo HTTP client so tests drive the source without a network.
	ClientFactory tempo.ClientFactory
	// Log receives one bounded JSON diagnostic line per claim; nil discards
	// it. The binary points it at stderr, which the extension host's process
	// runner does not surface (it keeps only a byte count), so the line
	// helps a local run and a test, not an operator in production.
	Log io.Writer
}

func (h Handler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h Handler) getenv(key string) string {
	if h.Getenv != nil {
		return h.Getenv(key)
	}
	return ""
}

// Handle collects the Tempo target named by the claim and returns the SDK
// result the host validates and commits. It never returns facts together
// with a failure state, never lets a credential into a result, and echoes
// the claim unchanged so the host's identity check passes. A denied or
// missing producer grant fails closed as terminal invalid_config before any
// provider contact.
func (h Handler) Handle(ctx context.Context, request Request) sdk.Result {
	started := h.now()
	claim := request.Claim

	parsed, err := parseSettings(request.Config, h.getenv)
	if err != nil {
		h.logLine(claim, FailureInvalidConfig, "")
		return terminalResult(claim, started, FailureInvalidConfig)
	}
	if err := grant.Check(parsed.grant, started); err != nil {
		h.logLine(claim, FailureInvalidConfig, "")
		return terminalResult(claim, started, FailureInvalidConfig)
	}
	source, err := tempo.NewClaimedSource(tempo.SourceConfig{
		CollectorInstanceID: claim.InstanceID,
		Targets:             parsed.targets,
		ClientFactory:       h.ClientFactory,
		Now:                 h.Now,
	})
	if err != nil {
		h.logLine(claim, FailureInvalidConfig, "")
		return terminalResult(claim, started, FailureInvalidConfig)
	}
	result, err := source.Collect(ctx, claim, parsed.grant)
	if err == nil {
		return result
	}
	var providerFailure transport.ProviderFailure
	if errors.As(err, &providerFailure) {
		h.logLine(claim, providerFailure.FailureClass(), providerFailure.FailureOperation())
		return providerResult(claim, started, providerFailure)
	}
	if isDeadlineExceeded(err) {
		h.logLine(claim, FailureClaimDeadline, "")
		return failureResult(claim, started, FailureClaimDeadline, false, 0)
	}
	h.logLine(claim, FailureInvalidEvidence, "")
	return terminalResult(claim, started, FailureInvalidEvidence)
}

// logLine writes one bounded diagnostic line: the claim identity the host
// already knows plus the failure vocabulary. It never carries a credential,
// a URL, or a provider body.
func (h Handler) logLine(claim sdk.Claim, class, operation string) {
	if h.Log == nil {
		return
	}
	line, err := json.Marshal(map[string]any{
		"component_id":  claim.ComponentID,
		"instance_id":   claim.InstanceID,
		"scope_id":      claim.Scope.ID,
		"work_item_id":  claim.WorkItemID,
		"failure_class": class,
		"operation":     operation,
	})
	if err != nil {
		return
	}
	_, _ = h.Log.Write(append(line, '\n'))
}
