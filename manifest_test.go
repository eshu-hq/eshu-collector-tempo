// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// manifestFacts is the subset of manifest.yaml this repository asserts: the
// component identity, the SDK protocol, digest-pinned placeholder artifacts,
// and the emitted fact kinds with their schema versions and source
// confidence. The manifest is hand-maintained and tiny; a line scanner keeps
// this package free of a YAML dependency for one gate.
type manifestFacts struct {
	apiVersion        string
	kind              string
	id                string
	version           string
	sdkProtocol       string
	adapter           string
	collectorKinds    []string
	artifactImages    []string
	emittedKinds      map[string][]string
	emittedConfidence map[string][]string
}

// loadManifestFacts scans manifest.yaml for the asserted subset.
func loadManifestFacts(t *testing.T) manifestFacts {
	t.Helper()

	raw, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	facts := manifestFacts{
		emittedKinds:      map[string][]string{},
		emittedConfidence: map[string][]string{},
	}
	var (
		inEmitted    bool
		inList       string // "versions" or "confidence" for the current kind
		currentKind  string
		currentImage bool
	)
	_ = currentImage
	for _, line := range strings.Split(string(raw), "\n") {
		stripped := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		// A new two-space spec section ends the emittedFacts block.
		if indent == 2 && strings.HasSuffix(stripped, ":") && stripped != "emittedFacts:" {
			inEmitted = false
			inList = ""
		}
		if stripped == "emittedFacts:" {
			inEmitted = true
			inList = ""
			continue
		}
		switch {
		case strings.HasPrefix(stripped, "apiVersion:"):
			facts.apiVersion = fieldValue(stripped, "apiVersion:")
		case strings.HasPrefix(stripped, "kind:") && facts.kind == "":
			facts.kind = fieldValue(stripped, "kind:")
		case strings.HasPrefix(stripped, "id:") && facts.id == "":
			facts.id = fieldValue(stripped, "id:")
		case strings.HasPrefix(stripped, "version:") && facts.version == "" && !inEmitted:
			facts.version = fieldValue(stripped, "version:")
		case strings.HasPrefix(stripped, "sdkProtocol:"):
			facts.sdkProtocol = fieldValue(stripped, "sdkProtocol:")
		case strings.HasPrefix(stripped, "adapter:"):
			facts.adapter = fieldValue(stripped, "adapter:")
		case strings.HasPrefix(stripped, "image:"):
			facts.artifactImages = append(facts.artifactImages, fieldValue(stripped, "image:"))
		}
		if !inEmitted {
			if strings.HasPrefix(stripped, "- ") {
				if v := fieldValue(stripped, "-"); v == CollectorKind {
					facts.collectorKinds = append(facts.collectorKinds, v)
				}
			}
			continue
		}
		switch {
		case strings.HasPrefix(stripped, "- kind:"):
			currentKind = fieldValue(stripped, "- kind:")
			facts.emittedKinds[currentKind] = nil
			inList = ""
		case strings.HasPrefix(stripped, "schemaVersions:"):
			inList = "versions"
			facts.emittedKinds[currentKind] = appendInlineList(facts.emittedKinds[currentKind], stripped)
			if len(facts.emittedKinds[currentKind]) > 0 {
				inList = ""
			}
		case strings.HasPrefix(stripped, "sourceConfidence:"):
			inList = "confidence"
			facts.emittedConfidence[currentKind] = appendInlineList(facts.emittedConfidence[currentKind], stripped)
			if len(facts.emittedConfidence[currentKind]) > 0 {
				inList = ""
			}
		case strings.HasPrefix(stripped, "- ") && inList != "" && currentKind != "":
			v := fieldValue(stripped, "-")
			if inList == "versions" {
				facts.emittedKinds[currentKind] = append(facts.emittedKinds[currentKind], v)
			} else {
				facts.emittedConfidence[currentKind] = append(facts.emittedConfidence[currentKind], v)
			}
		case strings.HasSuffix(stripped, ":"):
			inList = ""
		}
	}
	return facts
}

// fieldValue returns the scalar after a "key:" or "-" prefix with surrounding
// quotes and whitespace trimmed.
func fieldValue(line, prefix string) string {
	return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prefix)), "\"'")
}

// appendInlineList appends flow-style ["a", "b"] items found on one line.
func appendInlineList(dst []string, line string) []string {
	start, end := strings.Index(line, "["), strings.Index(line, "]")
	if start < 0 || end < 0 || end < start {
		return dst
	}
	for _, item := range strings.Split(line[start+1:end], ",") {
		if v := strings.Trim(item, " \"'"); v != "" {
			dst = append(dst, v)
		}
	}
	return dst
}

// digestPinned reports whether image carries a full sha256 digest reference.
func digestPinned(image string) bool {
	at := strings.LastIndex(image, "@sha256:")
	if at < 0 {
		return false
	}
	digest := image[at+len("@sha256:"):]
	if len(digest) != 64 {
		return false
	}
	for _, c := range digest {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// placeholderDigest is the artifact digest the repository manifest carries
// permanently. manifest.yaml is go:embed'ed into the binary, so a digest
// written into it would belong to an older build than the image that embeds
// it. The deployed digest is stamped at deploy time by `collector seed
// --image-ref` instead (README, "Image and seeding").
const placeholderDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

// isPlaceholderImage reports whether image names the placeholder digest.
func isPlaceholderImage(image string) bool {
	return strings.HasSuffix(image, "@"+placeholderDigest)
}

// TestManifestMatchesContract proves the component manifest matches the
// collector contract: the task-pinned id, the SDK protocol, digest-pinned
// placeholder artifacts, and exactly the three emitted observability kinds
// at 1.0.0 reported.
// reported.
func TestManifestMatchesContract(t *testing.T) {
	t.Parallel()

	manifest := loadManifestFacts(t)
	if manifest.apiVersion != "eshu.dev/v1alpha1" {
		t.Fatalf("apiVersion = %q, want eshu.dev/v1alpha1", manifest.apiVersion)
	}
	if manifest.kind != "ComponentPackage" {
		t.Fatalf("kind = %q, want ComponentPackage", manifest.kind)
	}
	if manifest.id != ComponentID {
		t.Fatalf("metadata.id = %q, want %q", manifest.id, ComponentID)
	}
	if manifest.id != "dev.eshu.collector.tempo" {
		t.Fatalf("metadata.id = %q, want the task-pinned dev.eshu.collector.tempo", manifest.id)
	}
	if manifest.version != "0.1.0" {
		t.Fatalf("metadata.version = %q, want 0.1.0", manifest.version)
	}
	if manifest.sdkProtocol != "collector-sdk/v1alpha1" {
		t.Fatalf("sdkProtocol = %q, want collector-sdk/v1alpha1", manifest.sdkProtocol)
	}
	if manifest.adapter != "process" {
		t.Fatalf("adapter = %q, want process", manifest.adapter)
	}
	if !reflect.DeepEqual(manifest.collectorKinds, []string{CollectorKind}) {
		t.Fatalf("collectorKinds = %v, want [%s]", manifest.collectorKinds, CollectorKind)
	}
	if len(manifest.artifactImages) == 0 {
		t.Fatal("manifest declares no artifacts")
	}
	for _, image := range manifest.artifactImages {
		if !digestPinned(image) {
			t.Fatalf("artifact image %q is not digest-pinned", image)
		}
		if strings.Contains(image, ":latest") {
			t.Fatalf("artifact image %q uses a mutable tag", image)
		}
		if !isPlaceholderImage(image) {
			t.Fatalf("artifact image %q is not the placeholder digest: do not commit a real digest "+
				"(the manifest is embedded in the image); stamp the deployed one with collector seed --image-ref", image)
		}
	}
	wantKinds := map[string][]string{}
	for _, kind := range FactKinds() {
		wantKinds[kind] = []string{SchemaVersion}
	}
	if !reflect.DeepEqual(manifest.emittedKinds, wantKinds) {
		t.Fatalf("manifest kinds = %v, contract kinds = %v", manifest.emittedKinds, wantKinds)
	}
	for kind, confidence := range manifest.emittedConfidence {
		if !reflect.DeepEqual(confidence, []string{"reported"}) {
			t.Fatalf("kind %q confidence=%v, want [reported]", kind, confidence)
		}
	}
	wantOrderedKinds := []string{
		"observability.source_instance",
		"observability.observed_trace_signal",
		"observability.coverage_warning",
	}
	if !reflect.DeepEqual(FactKinds(), wantOrderedKinds) {
		t.Fatalf("FactKinds() = %v, want exactly %v", FactKinds(), wantOrderedKinds)
	}
}
