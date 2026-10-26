// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

var fallbackClaimSequence uint64

// newClaimID mints a producer-namespaced claim identity. Consecutive IDs
// are unique per acquisition; the tempo-claim- prefix namespaces the
// claim to this producer family.
//
// Host-wiring home: only the unit test calls newClaimID in this repo.
// The host service wires it to claim acquisition (the in-tree
// service.go equivalent) at cutover; see README "Deferred to the host".
func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "tempo-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("tempo-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}
