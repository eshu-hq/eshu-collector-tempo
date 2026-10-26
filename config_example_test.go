// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestConfigExampleIsAValidActivation proves config.example.yaml stays in step
// with the config parser and the host-side claim identity: the declared scope
// matches the first target's scope_id, the source system and scope kind match
// what the collector emits, and the config parses with resolvable
// credentials.
func TestConfigExampleIsAValidActivation(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("config.example.yaml")
	if err != nil {
		t.Fatalf("os.ReadFile(config.example.yaml) error = %v", err)
	}
	var activation struct {
		Host struct {
			SourceSystem string `yaml:"sourceSystem"`
			Scope        struct {
				ID   string `yaml:"id"`
				Kind string `yaml:"kind"`
			} `yaml:"scope"`
		} `yaml:"host"`
		Process struct {
			Command string `yaml:"command"`
		} `yaml:"process"`
		Config map[string]any `yaml:"config"`
	}
	if err := yaml.Unmarshal(raw, &activation); err != nil {
		t.Fatalf("yaml.Unmarshal(config.example.yaml) error = %v", err)
	}
	if activation.Host.SourceSystem != SourceSystem || activation.Host.Scope.Kind != ScopeKind {
		t.Errorf("host = %+v, want source system %q and scope kind %q", activation.Host, SourceSystem, ScopeKind)
	}
	if activation.Process.Command == "" {
		t.Error("process.command is blank")
	}
	parsed, err := parseSettings(activation.Config, func(key string) string {
		return map[string]string{"TEMPO_TOKEN": "x", "TEMPO_TENANT_ID": "x"}[key]
	})
	if err != nil {
		t.Fatalf("parseSettings(example) error = %v", err)
	}
	if len(parsed.targets) == 0 {
		t.Fatal("example config parses to no targets")
	}
	if parsed.targets[0].ScopeID != activation.Host.Scope.ID {
		t.Errorf("config scope_id = %q, host.scope.id = %q, want them equal",
			parsed.targets[0].ScopeID, activation.Host.Scope.ID)
	}
}
