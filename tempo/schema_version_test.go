// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"testing"
)

// TestSchemaVersionAdmitsOnlyEmittedKinds proves the locked 1.0.0 version
// resolves for every emitted kind while declared-only kinds stay unknown.
func TestSchemaVersionAdmitsOnlyEmittedKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{KindSourceInstance, KindObservedTraceSignal, KindCoverageWarning} {
		version, ok := SchemaVersionForKind(kind)
		if !ok || version != SchemaVersionV1 {
			t.Fatalf("SchemaVersionForKind(%q) = (%q, %v), want (1.0.0, true)", kind, version, ok)
		}
	}
	if version, ok := SchemaVersionForKind("declared_thing"); ok || version != "" {
		t.Fatalf("SchemaVersionForKind(declared) = (%q, %v), want (\"\", false)", version, ok)
	}
}

// TestCheckSchemaVersionFailsClosed proves unknown kinds and foreign
// versions error while the locked version admits every emitted kind.
func TestCheckSchemaVersionFailsClosed(t *testing.T) {
	t.Parallel()

	if err := CheckSchemaVersion("declared_thing", SchemaVersionV1); err == nil {
		t.Fatal("CheckSchemaVersion(unknown kind) = nil, want non-nil")
	}
	if err := CheckSchemaVersion(KindObservedTraceSignal, "2.0.0"); err == nil {
		t.Fatal("CheckSchemaVersion(foreign version) = nil, want non-nil")
	}
	if err := CheckSchemaVersion(KindSourceInstance, SchemaVersionV1); err != nil {
		t.Fatalf("CheckSchemaVersion(locked) error = %v, want nil", err)
	}
}
