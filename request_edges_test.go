// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// errReader fails every read so request decoding can prove its read error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("test read failure") }

// TestParseRequestRejectsUnreadableInput proves a failing stdin surfaces a
// read error instead of an empty request.
func TestParseRequestRejectsUnreadableInput(t *testing.T) {
	t.Parallel()

	if _, err := ParseRequest(errReader{}); err == nil || !strings.Contains(err.Error(), "read request") {
		t.Fatalf("ParseRequest() error = %v, want a read failure", err)
	}
}

// TestParseRequestRejectsOversizeInput proves a document past the stdin cap
// fails before decoding.
func TestParseRequestRejectsOversizeInput(t *testing.T) {
	t.Parallel()

	base, err := marshalRequestForTest()
	if err != nil {
		t.Fatal(err)
	}
	padded := base + strings.Repeat(" ", MaxRequestBytes)
	if _, err := ParseRequest(stringReader(padded)); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("ParseRequest() error = %v, want an oversize failure", err)
	}
}

// TestParseRequestRejectsForeignContract proves a contract stanza on a
// foreign protocol version fails closed.
func TestParseRequestRejectsForeignContract(t *testing.T) {
	t.Parallel()

	base, err := marshalRequestForTest()
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(base), &raw); err != nil {
		t.Fatal(err)
	}
	raw["contract"] = map[string]any{"protocol_version": "v9"}
	mutated, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseRequest(stringReader(string(mutated)))
	if err == nil || !strings.Contains(err.Error(), "contract protocol_version") {
		t.Fatalf("ParseRequest() error = %v, want a contract version failure", err)
	}
}

// TestParseRequestRequiresClaimLiveness proves a zero attempt and a missing
// deadline each fail, so every parsed request can produce a valid result.
func TestParseRequestRequiresClaimLiveness(t *testing.T) {
	t.Parallel()

	base, err := marshalRequestForTest()
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(base), &raw); err != nil {
		t.Fatal(err)
	}
	claim, ok := raw["claim"].(map[string]any)
	if !ok {
		t.Fatal("valid request carries no claim object")
	}
	for _, row := range []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{"zero attempt", func(c map[string]any) { c["attempt"] = float64(0) }, "claim.attempt must be >= 1"},
		{"zero deadline", func(c map[string]any) { c["deadline"] = "0001-01-01T00:00:00Z" }, "claim.deadline is required"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			edited := map[string]any{}
			for key, value := range raw {
				edited[key] = value
			}
			claimCopy := map[string]any{}
			for key, value := range claim {
				claimCopy[key] = value
			}
			row.mutate(claimCopy)
			edited["claim"] = claimCopy
			mutated, err := json.Marshal(edited)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ParseRequest(stringReader(string(mutated)))
			if err == nil || !strings.Contains(err.Error(), row.wantErr) {
				t.Fatalf("ParseRequest() error = %v, want containing %q", err, row.wantErr)
			}
		})
	}
}

// TestParseRequestLabelsMalformedJSON proves a syntactically broken document
// fails with the friendly malformed reason instead of a raw decoder error.
func TestParseRequestLabelsMalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := ParseRequest(stringReader("not json"))
	if err == nil || !strings.Contains(err.Error(), "malformed JSON") {
		t.Fatalf("ParseRequest() error = %v, want a malformed-JSON failure", err)
	}
}

// TestParseRequestNamesTypeErrors proves a mistyped field names the field
// while an unknown field falls back to the decoder message.
func TestParseRequestNamesTypeErrors(t *testing.T) {
	t.Parallel()

	_, err := ParseRequest(stringReader(`{"protocol_version": 42}`))
	if err == nil || !strings.Contains(err.Error(), "has the wrong type") {
		t.Fatalf("ParseRequest() error = %v, want a field type failure", err)
	}
	base, merr := marshalRequestForTest()
	if merr != nil {
		t.Fatal(merr)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(base), &raw); err != nil {
		t.Fatal(err)
	}
	raw["unknown_field"] = true
	mutated, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseRequest(stringReader(string(mutated)))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("ParseRequest() error = %v, want an unknown-field failure", err)
	}
}
