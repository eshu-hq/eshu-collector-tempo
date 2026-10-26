// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	sdk "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu-collector-tempo/tempo"
)

const (
	// ComponentID is the manifest identity of this collector. Keep it in sync
	// with metadata.id in manifest.yaml.
	ComponentID = "dev.eshu.collector.tempo"
	// CollectorKind is the durable collector family name for Tempo
	// trace-signal facts.
	CollectorKind = tempo.CollectorKind
	// SourceSystem is the source system name facts carry when the claim does
	// not name one. Hosted claims carry the activation's source system.
	SourceSystem = tempo.CollectorKind
	// ScopeKind is the scope kind the activation declares for one Tempo API
	// target.
	ScopeKind = tempo.ScopeKindTraceSource
	// SchemaVersion is the schema version of every emitted observability
	// fact.
	SchemaVersion = tempo.SchemaVersionV1
	// FactKindSourceInstance identifies one configured Tempo source.
	FactKindSourceInstance = tempo.KindSourceInstance
	// FactKindObservedTraceSignal identifies live Tempo tag, tag-value, and
	// freshness-probe observations.
	FactKindObservedTraceSignal = tempo.KindObservedTraceSignal
	// FactKindCoverageWarning identifies one source-local coverage warning.
	FactKindCoverageWarning = tempo.KindCoverageWarning
	// ProducerID is the first-party producer identity. Production grants are
	// issued by core and enforced by the host; the satellite carries no
	// grant material.
	ProducerID = "dev.eshu.collector.tempo"
)

// FactKinds returns every fact kind this collector may emit, in the order the
// manifest declares them.
func FactKinds() []string {
	return []string{
		FactKindSourceInstance,
		FactKindObservedTraceSignal,
		FactKindCoverageWarning,
	}
}

// Contract returns the SDK fact families this collector may emit.
func Contract() sdk.Contract {
	kinds := FactKinds()
	facts := make([]sdk.FactDeclaration, 0, len(kinds))
	for _, kind := range kinds {
		facts = append(facts, sdk.FactDeclaration{
			Kind:             kind,
			SchemaVersions:   []string{SchemaVersion},
			SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
		})
	}
	return sdk.Contract{ProtocolVersion: sdk.ProtocolVersionV1Alpha1, Facts: facts}
}
