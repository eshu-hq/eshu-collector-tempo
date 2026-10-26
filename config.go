// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu-collector-tempo/tempo"
)

const (
	// DefaultMaxResultBytes keeps one result under the host's default 4 MiB
	// standard-output limit.
	DefaultMaxResultBytes = 3500000
	// maxResultBytesCeiling bounds an operator-raised max_result_bytes.
	maxResultBytesCeiling = 64 << 20
)

// targetJSON is the activation config shape for one Tempo API target. It
// mirrors the in-tree claimed runtime configuration (eshu-hq/eshu
// go/cmd/collector-tempo config.go): the same fields, the same token_env and
// tenant_id_env credential indirection, and the same fail-closed credential
// rules. The token stays optional: an empty token_env means unauthenticated
// Tempo, while a named-but-unset variable fails closed before any provider
// contact. stale_after is accepted and validated for activation parity but is
// core-owned and never reaches the producer: the extracted TargetConfig
// carries no StaleAfter field (see the satellite README, "Boundaries").
// provider carries the host-passed per-target provider key: the host always
// sends "tempo" here, and anything else fails closed as a typo.
type targetJSON struct {
	Provider              string   `json:"provider"`
	ScopeID               string   `json:"scope_id"`
	InstanceID            string   `json:"instance_id"`
	BaseURL               string   `json:"base_url"`
	PathPrefix            string   `json:"path_prefix"`
	TokenEnv              string   `json:"token_env"`
	TenantID              string   `json:"tenant_id"`
	TenantIDEnv           string   `json:"tenant_id_env"`
	ResourceLimit         int      `json:"resource_limit"`
	TagValueNames         []string `json:"tag_value_names"`
	MaxTagValuesPerTag    int      `json:"max_tag_values_per_tag"`
	StaleAfter            string   `json:"stale_after"`
	Enabled               bool     `json:"enabled"`
	DeclaredIDs           []string `json:"declared_ids"`
	ObservedOnlyHint      bool     `json:"observed_only_hint"`
	FreshnessProbeEnabled bool     `json:"freshness_probe_enabled"`
	Lookback              string   `json:"lookback"`
}

// limitsJSON is the shared host-level limits stanza. Only maxResultBytes has
// a satellite-side consumer: it bounds the encoded result so the satellite's
// own bound matches what the operator approved. The rest are accepted so the
// verbatim envelope parses and host-side enforcement stays the single owner.
// No grant material ever arrives here: core-owned-kind authorization is
// host-enforced at emission.
type limitsJSON struct {
	MaxRecordsPerClaim  int `json:"maxRecordsPerClaim"`
	MaxPayloadBytes     int `json:"maxPayloadBytes"`
	MaxResultBytes      int `json:"maxResultBytes"`
	ClaimTimeoutSeconds int `json:"claimTimeoutSeconds"`
}

// settings is the parsed activation: the producer targets plus the encoded
// result bound. The host delivers the activation config block verbatim,
// including its shared limits stanza; authorization material never arrives
// here because core-owned-kind authorization is host-enforced at emission.
type settings struct {
	targets        []tempo.TargetConfig
	maxResultBytes int
}

// parseSettings decodes the activation config into producer targets and the
// encoded result bound. The claim's instance id becomes the producer's
// collector identity. Credential values never enter errors: a missing token
// names the variable, never its content.
func parseSettings(config map[string]any, getenv func(string) string) (settings, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return settings{}, fmt.Errorf("encode config: %w", err)
	}
	var decoded struct {
		Targets []targetJSON `json:"targets"`
		Limits  *limitsJSON  `json:"limits"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return settings{}, fmt.Errorf("decode config: %s", decodeReason(err))
	}
	if len(decoded.Targets) == 0 {
		return settings{}, fmt.Errorf("config targets are required")
	}
	targets := make([]tempo.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return settings{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	maxResultBytes := 0
	if decoded.Limits != nil {
		maxResultBytes = decoded.Limits.MaxResultBytes
	}
	if maxResultBytes < 0 || maxResultBytes > maxResultBytesCeiling {
		return settings{}, fmt.Errorf("limits.maxResultBytes must be between 0 and %d", maxResultBytesCeiling)
	}
	if maxResultBytes == 0 {
		maxResultBytes = DefaultMaxResultBytes
	}
	return settings{targets: targets, maxResultBytes: maxResultBytes}, nil
}

// mapTarget resolves one configured target against the process environment,
// mirroring the in-tree claimed runtime mapping. An empty token_env means
// unauthenticated Tempo; a named-but-unset variable fails closed. tenant_id
// is overridden by tenant_id_env when the latter names a set variable. The
// parsed stale_after is validated and dropped: freshness windowing stays
// core-owned and the producer TargetConfig carries no such field. A
// non-empty provider must name tempo.
func mapTarget(target targetJSON, getenv func(string) string) (tempo.TargetConfig, error) {
	if provider := strings.TrimSpace(target.Provider); provider != "" && provider != tempo.ProviderTempo {
		return tempo.TargetConfig{}, fmt.Errorf("unsupported tempo provider %q", target.Provider)
	}
	token, err := optionalEnvValue(target.TokenEnv, getenv, "token_env")
	if err != nil {
		return tempo.TargetConfig{}, err
	}
	tenantID, err := optionalEnvOverride(target.TenantID, target.TenantIDEnv, getenv, "tenant_id_env")
	if err != nil {
		return tempo.TargetConfig{}, err
	}
	if _, err := parseOptionalDuration(target.StaleAfter, "stale_after"); err != nil {
		return tempo.TargetConfig{}, err
	}
	lookback, err := parseOptionalDuration(target.Lookback, "lookback")
	if err != nil {
		return tempo.TargetConfig{}, err
	}
	return tempo.TargetConfig{
		ScopeID:              strings.TrimSpace(target.ScopeID),
		InstanceID:           strings.TrimSpace(target.InstanceID),
		BaseURL:              strings.TrimRight(strings.TrimSpace(target.BaseURL), "/"),
		PathPrefix:           strings.TrimSpace(target.PathPrefix),
		Token:                token,
		TenantID:             tenantID,
		ResourceLimit:        target.ResourceLimit,
		TagValueNames:        cleanStrings(target.TagValueNames),
		MaxTagValuesPerTag:   target.MaxTagValuesPerTag,
		Enabled:              target.Enabled,
		DeclaredIDs:          stringSet(target.DeclaredIDs),
		ObservedOnlyHint:     target.ObservedOnlyHint,
		FreshnessProbeEnable: target.FreshnessProbeEnabled,
		Lookback:             lookback,
	}, nil
}

// optionalEnvValue resolves a credential variable name: empty means unset
// (unauthenticated for token_env), set-but-empty fails closed naming the
// variable, never its content. A nil getenv resolves nothing, so tests and
// the handler fail closed without ambient authority.
func optionalEnvValue(envName string, getenv func(string) string, field string) (string, error) {
	return optionalEnvOverride("", envName, getenv, field)
}

// optionalEnvOverride resolves a raw value overridden by a credential
// variable: when envName is empty the raw value stands, otherwise the named
// variable must resolve to a non-blank value.
func optionalEnvOverride(raw, envName string, getenv func(string) string, field string) (string, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return strings.TrimSpace(raw), nil
	}
	var value string
	if getenv != nil {
		value = strings.TrimSpace(getenv(envName))
	}
	if value == "" {
		return "", fmt.Errorf("%s %s did not resolve a value", field, envName)
	}
	return value, nil
}

// parseOptionalDuration parses an optional duration field: empty means unset,
// otherwise a non-negative Go duration.
func parseOptionalDuration(raw, field string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", field, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative", field)
	}
	return value, nil
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func stringSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out[trimmed] = struct{}{}
		}
	}
	return out
}
