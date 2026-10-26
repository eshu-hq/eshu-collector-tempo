// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu-sdk/factschema"
)

// SchemaVersionV1 is the first observability evidence schema. All three
// emitted kinds are at 1.0.0 today (EXTRACTION_PREP census §4 in
// eshu-hq/eshu, go/internal/collector/observability/tempo).
//
// P1 SDK GAP: the published factschema v0.1.0-rc.5 module exposes the fact
// kind strings (factschema.FactKindObservability*) and the contract payload
// encoders, but no per-kind schema-version map and no ValidateSchemaVersion
// admission helper (compare ObservabilitySchemaVersion and
// schema_version.go ValidateSchemaVersion in eshu-hq/eshu
// go/internal/facts). Until the SDK gains that helper, this producer carries
// the locked version locally and fails closed on anything else. Do NOT widen
// this map without a core-qualified SDK pin.
const SchemaVersionV1 = "1.0.0"

// KindSourceInstance identifies one configured Tempo source.
const KindSourceInstance = factschema.FactKindObservabilitySourceInstance

// KindObservedTraceSignal identifies live Tempo tag, tag-value, and
// freshness-probe observations.
const KindObservedTraceSignal = factschema.FactKindObservabilityObservedTraceSignal

// KindCoverageWarning identifies source-local coverage warnings.
const KindCoverageWarning = factschema.FactKindObservabilityCoverageWarning

var emittedKinds = map[string]struct{}{
	KindSourceInstance:      {},
	KindObservedTraceSignal: {},
	KindCoverageWarning:     {},
}

// SchemaVersionForKind returns the locked schema version for an emitted
// kind. Declared kinds are absent: this producer must never emit them.
func SchemaVersionForKind(kind string) (string, bool) {
	if _, ok := emittedKinds[kind]; !ok {
		return "", false
	}
	return SchemaVersionV1, true
}

// CheckSchemaVersion admits only the locked 1.0.0 version for emitted
// kinds and fails closed on unknown kinds and foreign versions.
func CheckSchemaVersion(kind, version string) error {
	locked, ok := SchemaVersionForKind(kind)
	if !ok {
		return fmt.Errorf("tempo fact kind %q is not emitted by this producer", kind)
	}
	if strings.TrimSpace(version) != locked {
		return fmt.Errorf(
			"fact kind %q schema_version %q is unsupported; producer supports %q",
			kind,
			strings.TrimSpace(version),
			locked,
		)
	}
	return nil
}
