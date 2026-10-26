// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"strings"
	"testing"
)

func TestClaimIDNamespace(t *testing.T) {
	t.Parallel()

	first, second := newClaimID(), newClaimID()
	for _, id := range []string{first, second} {
		if !strings.HasPrefix(id, "tempo-claim-") {
			t.Errorf("newClaimID() = %q, want tempo-claim- namespace", id)
		}
	}
	if first == second {
		t.Error("newClaimID() repeated without uniqueness")
	}
}
