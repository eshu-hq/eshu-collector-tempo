// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"

	collector "github.com/eshu-hq/eshu-collector-tempo"
	"github.com/eshu-hq/eshu-collector-tempo/tempo"
)

// stubClient serves one canned Tempo metadata snapshot without a network.
type stubClient struct {
	snapshot tempo.CollectionResult
}

func (s stubClient) CollectObservedMetadata(
	context.Context, tempo.TargetConfig,
) (tempo.CollectionResult, error) {
	return s.snapshot, nil
}

func fixtureSnapshot() tempo.CollectionResult {
	return tempo.CollectionResult{
		Source: tempo.SourceInstance{
			Provider:         tempo.ProviderTempo,
			SourceInstanceID: "tempo-example",
		},
		Signals: []tempo.TraceSignal{{
			ProviderObjectID: "tagvalues:resource.service.name",
			SignalKind:       tempo.SignalKindTagValues,
			TagScope:         "resource",
			TagName:          "resource.service.name",
			TagValueCount:    2,
			TagValueHashes:   []string{"recorded-a", "recorded-b"},
		}},
		ObservedAt: time.Now().UTC().Truncate(time.Second),
	}
}

func stubFactory(snapshot tempo.CollectionResult) tempo.ClientFactory {
	return func(tempo.TargetConfig) (tempo.EvidenceClient, error) {
		return stubClient{snapshot: snapshot}, nil
	}
}

func serveEnv() map[string]string {
	return map[string]string{"TEMPO_TOKEN": "token-value"}
}

func claimJSON(t *testing.T, scopeID string) string {
	t.Helper()

	now := time.Now().UTC()
	raw, err := json.Marshal(collector.Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim: sdk.Claim{
			ComponentID:   collector.ComponentID,
			InstanceID:    "tempo-primary",
			CollectorKind: collector.CollectorKind,
			SourceSystem:  collector.CollectorKind,
			Scope:         sdk.Scope{ID: scopeID, Kind: "trace_source"},
			SourceRunID:   "run-1",
			GenerationID:  "tempo:generation-9",
			WorkItemID:    "work-1",
			FencingToken:  "7",
			Attempt:       1,
			Deadline:      now.Add(time.Hour).UTC(),
			ConfigHandle:  "component-config:tempo",
		},
		Contract: collector.Contract(),
		Config: map[string]any{
			"targets": []any{map[string]any{
				"provider":               "tempo",
				"scope_id":               "tempo:trace:example-prod",
				"instance_id":            "tempo-example",
				"base_url":               "https://tempo.example.test",
				"token_env":              "TEMPO_TOKEN",
				"resource_limit":         float64(50),
				"max_tag_values_per_tag": float64(20),
				"enabled":                true,
				"lookback":               "1h",
			}},
			"limits": map[string]any{
				"maxRecordsPerClaim":  float64(50000),
				"maxPayloadBytes":     float64(1048576),
				"maxResultBytes":      float64(3500000),
				"claimTimeoutSeconds": float64(300),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func getenv(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

// TestRunServesOneClaimFromStdin serves one claim through run and asserts
// stdout carries exactly one complete SDK result that passes the published
// validator.
func TestRunServesOneClaimFromStdin(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"--sdk-stdio"}} {
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), args,
			strings.NewReader(claimJSON(t, "tempo:trace:example-prod")),
			&stdout, &stderr, getenv(serveEnv()), time.Now, stubFactory(fixtureSnapshot()))
		if err != nil {
			t.Fatalf("run(%v) error = %v", args, err)
		}
		decoder := json.NewDecoder(&stdout)
		decoder.DisallowUnknownFields()
		var result sdk.Result
		if err := decoder.Decode(&result); err != nil {
			t.Fatalf("stdout is not one SDK result: %v", err)
		}
		if decoder.More() {
			t.Fatal("stdout carries more than one JSON value")
		}
		if result.State != sdk.ResultComplete {
			t.Fatalf("State = %q, want complete", result.State)
		}
		if len(result.Facts) != 2 {
			t.Fatalf("facts = %d, want 2", len(result.Facts))
		}
		if _, err := sdk.NewValidator(collector.Contract()).ValidateResult(result); err != nil {
			t.Fatalf("ValidateResult() error = %v", err)
		}
	}
}

// TestRunFailsExpiredClaimRetryable proves a claim arriving with no lease
// time left ends retryable with no facts and no provider contact.
func TestRunFailsExpiredClaimRetryable(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	raw, err := json.Marshal(collector.Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim: sdk.Claim{
			ComponentID:   collector.ComponentID,
			InstanceID:    "tempo-primary",
			CollectorKind: collector.CollectorKind,
			SourceSystem:  collector.CollectorKind,
			Scope:         sdk.Scope{ID: "tempo:trace:example-prod", Kind: "trace_source"},
			SourceRunID:   "run-1",
			GenerationID:  "tempo:generation-9",
			WorkItemID:    "work-1",
			FencingToken:  "7",
			Attempt:       1,
			Deadline:      now.Add(-time.Minute).UTC(),
			ConfigHandle:  "component-config:tempo",
		},
		Contract: collector.Contract(),
		Config:   map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	contacted := false
	factory := func(tempo.TargetConfig) (tempo.EvidenceClient, error) {
		contacted = true
		return stubClient{}, nil
	}
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), nil, bytes.NewReader(raw), &stdout, &stderr,
		getenv(serveEnv()), time.Now, factory); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if contacted {
		t.Fatal("expired claim constructed a provider client")
	}
	var result sdk.Result
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		t.Fatalf("stdout is not one SDK result: %v", err)
	}
	if result.State != sdk.ResultRetryable {
		t.Fatalf("State = %q, want retryable", result.State)
	}
	var found bool
	for _, status := range result.Statuses {
		if status.Class == sdk.StatusFailure && status.FailureClass == collector.FailureClaimDeadline {
			found = true
		}
	}
	if !found {
		t.Fatalf("result %+v carries no claim_deadline_exceeded failure status", result.Statuses)
	}
}

// TestRunRejectsMalformedInput proves protocol failures exit non-zero with no
// result on stdout: an unknown argument and a non-JSON request.
func TestRunRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"unknown argument": {"--sdk-stdio", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), args, strings.NewReader(""),
			&stdout, &stderr, getenv(nil), time.Now, stubFactory(fixtureSnapshot())); err == nil {
			t.Fatalf("%s: run() error = nil, want non-nil", name)
		}
	}
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), nil, strings.NewReader("not json"),
		&stdout, &stderr, getenv(nil), time.Now, stubFactory(fixtureSnapshot())); err == nil {
		t.Fatal("malformed request: run() error = nil, want non-nil")
	}
	if stdout.Len() != 0 {
		t.Fatal("malformed request wrote a result to stdout")
	}
}

// TestSeedWritesBinaryAndManifest runs the seed subcommand into a temp dir
// and asserts the binary is executable and the manifest matches the embedded
// one.
func TestSeedWritesBinaryAndManifest(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"seed", "--dest", dest},
		strings.NewReader(""), &stdout, io.Discard, getenv(nil), time.Now, nil); err != nil {
		t.Fatalf("run(seed) error = %v", err)
	}
	binary := filepath.Join(dest, "collector")
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("seeded binary missing: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("seeded binary %s is not executable", binary)
	}
	manifest, err := os.ReadFile(filepath.Join(dest, "manifest.yaml"))
	if err != nil {
		t.Fatalf("seeded manifest missing: %v", err)
	}
	if !bytes.Equal(manifest, collector.ManifestYAML) {
		t.Fatal("seeded manifest differs from the embedded manifest")
	}
	if !strings.HasPrefix(stdout.String(), "seeded ") {
		t.Fatalf("seed printed %q, want a seeded summary line", stdout.String())
	}
}

// TestSeedStampsImageRef proves --image-ref (and the COLLECTOR_IMAGE_REF
// fallback) replaces the placeholder digest, and that a malformed reference
// fails before any file is written.
func TestSeedStampsImageRef(t *testing.T) {
	t.Parallel()

	ref := "ghcr.io/eshu-hq/eshu-collector-tempo@sha256:" + strings.Repeat("a", 64)
	dest := t.TempDir()
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"seed", "--dest", dest, "--image-ref", ref},
		strings.NewReader(""), &stdout, io.Discard, getenv(nil), time.Now, nil); err != nil {
		t.Fatalf("run(seed --image-ref) error = %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(dest, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), ref) {
		t.Fatal("seeded manifest does not carry the stamped image reference")
	}
	if strings.Contains(string(manifest), strings.Repeat("0", 64)) {
		t.Fatal("seeded manifest still carries the placeholder digest")
	}

	envDest := t.TempDir()
	var envStdout bytes.Buffer
	env := map[string]string{imageRefEnv: ref + "\n"}
	if err := run(context.Background(), []string{"seed", "--dest", envDest},
		strings.NewReader(""), &envStdout, io.Discard, getenv(env), time.Now, nil); err != nil {
		t.Fatalf("run(seed with $%s) error = %v", imageRefEnv, err)
	}
	envManifest, err := os.ReadFile(filepath.Join(envDest, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(envManifest), ref) {
		t.Fatal("seeded manifest does not carry the environment image reference")
	}

	badDest := t.TempDir()
	var badStdout bytes.Buffer
	if err := run(context.Background(), []string{"seed", "--dest", badDest, "--image-ref", "not-a-digest"},
		strings.NewReader(""), &badStdout, io.Discard, getenv(nil), time.Now, nil); err == nil {
		t.Fatal("run(seed --image-ref bogus) error = nil, want non-nil")
	}
	if entries, err := os.ReadDir(badDest); err != nil || len(entries) != 0 {
		t.Fatalf("malformed image reference wrote files into %s", badDest)
	}
}
