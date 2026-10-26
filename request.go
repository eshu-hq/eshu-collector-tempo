// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// MaxRequestBytes caps the stdin request document. The host sends one claim
// plus the activation config, which is far smaller than this.
const MaxRequestBytes = 1 << 20

// Request is the bounded JSON document the extension host writes to stdin. It
// matches the host's extension request wire shape.
type Request struct {
	ProtocolVersion string         `json:"protocol_version"`
	Claim           sdk.Claim      `json:"claim"`
	Contract        sdk.Contract   `json:"contract"`
	Config          map[string]any `json:"config,omitempty"`
}

// ParseRequest decodes and validates one host request. It reads at most
// MaxRequestBytes, rejects unknown fields and trailing JSON, and requires
// the claim identity the result must echo back to the host. Errors never
// quote the input.
func ParseRequest(r io.Reader) (Request, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxRequestBytes+1))
	if err != nil {
		return Request{}, fmt.Errorf("read request: %w", err)
	}
	if len(raw) > MaxRequestBytes {
		return Request{}, fmt.Errorf("request exceeds %d bytes", MaxRequestBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("decode request: %s", decodeReason(err))
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Request{}, errors.New("decode request: trailing JSON value")
	}
	if strings.TrimSpace(request.ProtocolVersion) != sdk.ProtocolVersionV1Alpha1 {
		return Request{}, fmt.Errorf("protocol_version %q is unsupported", request.ProtocolVersion)
	}
	if strings.TrimSpace(request.Contract.ProtocolVersion) != "" &&
		strings.TrimSpace(request.Contract.ProtocolVersion) != sdk.ProtocolVersionV1Alpha1 {
		return Request{}, fmt.Errorf("contract protocol_version %q is unsupported", request.Contract.ProtocolVersion)
	}
	if err := validateRequestClaim(request.Claim); err != nil {
		return Request{}, err
	}
	return request, nil
}

// validateRequestClaim requires every claim field the SDK result validator
// requires on the echoed claim, so a parsed request can always produce a
// result that passes validation. It never quotes the claim values.
func validateRequestClaim(claim sdk.Claim) error {
	required := map[string]string{
		"claim.component_id":   claim.ComponentID,
		"claim.instance_id":    claim.InstanceID,
		"claim.collector_kind": claim.CollectorKind,
		"claim.source_system":  claim.SourceSystem,
		"claim.scope.id":       claim.Scope.ID,
		"claim.scope.kind":     claim.Scope.Kind,
		"claim.source_run_id":  claim.SourceRunID,
		"claim.generation_id":  claim.GenerationID,
		"claim.work_item_id":   claim.WorkItemID,
		"claim.fencing_token":  claim.FencingToken,
		"claim.config_handle":  claim.ConfigHandle,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if claim.CollectorKind != CollectorKind {
		return fmt.Errorf("claim.collector_kind %q is not %q", claim.CollectorKind, CollectorKind)
	}
	if claim.Attempt < 1 {
		return errors.New("claim.attempt must be >= 1")
	}
	if claim.Deadline.IsZero() {
		return errors.New("claim.deadline is required")
	}
	return nil
}

func decodeReason(err error) string {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return "malformed JSON"
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Sprintf("field %q has the wrong type", typeErr.Field)
	}
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), "json: "))
}
