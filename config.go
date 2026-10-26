// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu-collector-tempo/grant"
	"github.com/eshu-hq/eshu-collector-tempo/tempo"
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
type targetJSON struct {
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

// kindGrantJSON is one covered core-owned fact kind in the host-delivered
// grant. The list form matches the in-tree producer-grant draft
// (producer-grant.manifest.DRAFT.yaml in eshu-hq/eshu go/cmd/collector-tempo).
type kindGrantJSON struct {
	Kind           string   `json:"kind"`
	SchemaVersions []string `json:"schema_versions"`
}

// grantJSON is the wire shape of the host-delivered core issuance the
// activation carries until the host defines grant delivery. It carries no
// secret: core matches the producer, version, scope, kind coverage, and
// expiry at admission, and this edge re-checks the shape at every emission.
// issued_at and expires_at bound the grant lifetime to 720h; the collector
// never mints one.
type grantJSON struct {
	ProducerID string          `json:"producer_id"`
	Version    string          `json:"version"`
	Scope      string          `json:"scope"`
	Kinds      []kindGrantJSON `json:"kinds"`
	IssuedAt   time.Time       `json:"issued_at"`
	ExpiresAt  time.Time       `json:"expires_at"`
}

// settings is the parsed activation: the producer targets plus the
// host-delivered grant the emission edge enforces.
type settings struct {
	targets []tempo.TargetConfig
	grant   grant.Grant
}

// parseSettings decodes the activation config into producer targets and the
// host-delivered grant. The claim's instance id becomes the producer's
// collector identity. Credential values never enter errors: a missing token
// names the variable, never its content.
func parseSettings(config map[string]any, getenv func(string) string) (settings, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return settings{}, fmt.Errorf("encode config: %w", err)
	}
	var decoded struct {
		Targets []targetJSON `json:"targets"`
		Grant   *grantJSON   `json:"grant"`
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
	if decoded.Grant == nil {
		return settings{}, fmt.Errorf("config grant is required: the host must deliver the core-issued producer grant")
	}
	mapped, err := mapGrant(decoded.Grant)
	if err != nil {
		return settings{}, fmt.Errorf("grant: %w", err)
	}
	return settings{targets: targets, grant: mapped}, nil
}

// mapTarget resolves one configured target against the process environment,
// mirroring the in-tree claimed runtime mapping. An empty token_env means
// unauthenticated Tempo; a named-but-unset variable fails closed. tenant_id
// is overridden by tenant_id_env when the latter names a set variable. The
// parsed stale_after is validated and dropped: freshness windowing stays
// core-owned and the producer TargetConfig carries no such field.
func mapTarget(target targetJSON, getenv func(string) string) (tempo.TargetConfig, error) {
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

// mapGrant converts the activation grant into the producer grant shape the
// emission edge enforces. Timestamps must bound a positive lifetime of at
// most 720h; grant.Check re-verifies every bound before provider contact.
func mapGrant(raw *grantJSON) (grant.Grant, error) {
	kinds := make([]grant.KindGrant, 0, len(raw.Kinds))
	for _, kind := range raw.Kinds {
		kinds = append(kinds, grant.KindGrant{
			Kind:           strings.TrimSpace(kind.Kind),
			SchemaVersions: append([]string(nil), kind.SchemaVersions...),
		})
	}
	return grant.Grant{
		ProducerID: strings.TrimSpace(raw.ProducerID),
		Version:    strings.TrimSpace(raw.Version),
		Scope:      strings.TrimSpace(raw.Scope),
		Kinds:      kinds,
		IssuedAt:   raw.IssuedAt.UTC(),
		ExpiresAt:  raw.ExpiresAt.UTC(),
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
