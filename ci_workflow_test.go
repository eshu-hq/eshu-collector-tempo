// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// reusableSHA pins the shared lane bodies: eshu-hq/.github
// go-collector-ci v2. Re-verify with git ls-remote when auditing;
// Dependabot github-actions proposes bumps, weekly grouped.
const reusableSHA = "4d01ab90baf5dd77c21cc1ee6bdf701332613449"

// callerPins are the only `uses:` references the thin caller may execute.
// Each entry pairs the reference stem with its SHA and version comment, so
// a moved tag, a comment-less bump, or a dropped pin fails the gate.
var callerPins = []struct {
	uses    string
	sha     string
	comment string
}{
	{"actions/checkout@", "3d3c42e5aac5ba805825da76410c181273ba90b1", "# v7.0.1"},
	{"dorny/paths-filter@", "ceb8a2b8f2d89434be7ff52d3de7ec3738c5cc9d", "# v4"},
	{"actions/download-artifact@", "37930b1c2abaa49bbe596cd826c3c89aef350131", "# v7"},
	{"github/codeql-action/upload-sarif@", "9b3254085494b55a0f7c4c2b3d2d52cc712ce370", "# v4.39.1"},
	{"eshu-hq/.github/.github/workflows/go-collector-ci.yml@", reusableSHA, "# v2"},
}

// callerPinProblems returns every way raw breaks the caller pin contract:
// each `uses:` line (step-level and the job-level reusable call) must carry
// a full commit SHA plus a version comment, and every caller pin above must
// be present verbatim.
func callerPinProblems(raw string) []string {
	var problems []string
	for _, line := range strings.Split(raw, "\n") {
		idx := strings.Index(line, "uses:")
		if idx < 0 {
			continue
		}
		uses := strings.TrimSpace(line[idx+len("uses:"):])
		ref := uses
		if i := strings.Index(ref, "#"); i >= 0 {
			ref = strings.TrimSpace(ref[:i])
		}
		if !pinnedAction.MatchString(ref) {
			problems = append(problems, fmt.Sprintf("uses %q is not a full commit SHA pin", uses))
			continue
		}
		if !strings.Contains(uses, "# v") {
			problems = append(problems, fmt.Sprintf("uses %q carries no version comment", uses))
		}
	}
	for _, pin := range callerPins {
		if !strings.Contains(raw, pin.uses+pin.sha+" "+pin.comment) {
			problems = append(problems, "caller lacks pin "+pin.uses+pin.sha+" "+pin.comment)
		}
	}
	return problems
}

// TestCIWorkflowPinsActionsByCommitSHA keeps a moved tag from changing what
// pull-request CI executes. The caller holds only contents: read, but it
// still runs third-party action code (and the shared reusable) on every
// pull request. The live ci.yml text must pass; each mutant must fail.
func TestCIWorkflowPinsActionsByCommitSHA(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if problems := callerPinProblems(string(raw)); len(problems) > 0 {
		t.Errorf("ci.yml breaks the caller pin contract:\n%s", strings.Join(problems, "\n"))
	}

	good := string(raw)
	for name, mutate := range map[string]func(string) string{
		// Moved-tag shape: the reusable floats instead of running pinned code.
		"reusable floats on v2 tag": func(s string) string {
			return strings.Replace(s, "go-collector-ci.yml@"+reusableSHA+" # v2", "go-collector-ci.yml@v2", 1)
		},
		// Wrong SHA: silently runs different upstream code.
		"reusable wrong SHA": func(s string) string {
			return strings.Replace(s, "go-collector-ci.yml@"+reusableSHA, "go-collector-ci.yml@0000000000000000000000000000000000000000", 1)
		},
		// Comment-less bump: the audit trail no longer names the version.
		"checkout pin drops its version comment": func(s string) string {
			return strings.Replace(s, "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1", "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", 1)
		},
	} {
		mutated := mutate(good)
		if mutated == good {
			t.Fatalf("%s: the mutation did not change the workflow", name)
		}
		if len(callerPinProblems(mutated)) == 0 {
			t.Errorf("%s: the gate accepted a pin violation", name)
		}
	}
}

// callerJobs are the only jobs the thin caller may define: the paths-filter
// changes job, the reusable go-ci call, and the two caller-side jobs the
// reusable cannot own (weekly findings issue, SARIF upload). Lane bodies
// (lint/test/race/shell/image/boundary/govulncheck/gosec/actionlint and the
// ci-complete aggregator) live upstream; check runs nest as `go-ci /
// <lane>`, so the ruleset requires `go-ci / ci-complete`.
var callerJobs = []string{"changes", "go-ci", "scheduled-findings", "gosec-sarif"}

// callerJobProblems returns every way raw breaks the caller job contract:
// the job set must be exactly the caller jobs, no more, no fewer.
func callerJobProblems(raw []byte) []string {
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return []string{"ci.yml does not parse: " + err.Error()}
	}
	var problems []string
	want := make(map[string]bool, len(callerJobs))
	for _, name := range callerJobs {
		want[name] = true
		if _, ok := workflow.Jobs[name]; !ok {
			problems = append(problems, "ci.yml has no "+name+" job, want exactly the caller jobs")
		}
	}
	for name := range workflow.Jobs {
		if !want[name] {
			problems = append(problems, "ci.yml has unexpected "+name+" job: lane bodies live in the reusable, not the caller")
		}
	}
	if len(workflow.Jobs) != len(callerJobs) {
		problems = append(problems, fmt.Sprintf("ci.yml defines %d jobs, want exactly %d caller jobs", len(workflow.Jobs), len(callerJobs)))
	}
	return problems
}

// TestCIWorkflowKeepsCallerJobNames keeps the thin-caller contract: exactly
// the four caller jobs exist, and no lane job leaked back into the caller.
// The live ci.yml text must pass; each mutant must fail.
func TestCIWorkflowKeepsCallerJobNames(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if problems := callerJobProblems(raw); len(problems) > 0 {
		t.Errorf("ci.yml breaks the caller job contract:\n%s", strings.Join(problems, "\n"))
	}

	good := string(raw)
	for name, mutate := range map[string]func(string) string{
		// Lane job leaks back into the caller: two owners for one lane.
		"lint lane leaks into the caller": func(s string) string {
			return strings.Replace(s, "  go-ci:", "  lint:\n    runs-on: ubuntu-latest\n    timeout-minutes: 15\n  go-ci:", 1)
		},
		// Dropped caller job: the weekly findings issue never files.
		"dropped scheduled-findings job": func(s string) string {
			return strings.Replace(s, "  scheduled-findings:", "  scheduled-findings-retired:", 1)
		},
	} {
		mutated := mutate(good)
		if mutated == good {
			t.Fatalf("%s: the mutation did not change the workflow", name)
		}
		if len(callerJobProblems([]byte(mutated))) == 0 {
			t.Errorf("%s: the gate accepted a job-set violation", name)
		}
	}
}

// callerTimeouts are the per-job ceilings for the plain caller-side jobs:
// 5 minutes each so a hung runner cannot hold a PR open. The go-ci
// reusable call carries no timeout: GitHub rejects timeout-minutes on a
// job-level `uses:` (actionlint syntax-check), so its ceiling lives in
// the upstream inner-lane timeouts.
var callerTimeouts = map[string]int{
	"changes":            5,
	"scheduled-findings": 5,
	"gosec-sarif":        5,
}

// callerTimeoutProblems returns every way raw breaks the caller timeout
// contract: every plain caller job declares exactly its ceiling, and the
// go-ci reusable call declares none.
func callerTimeoutProblems(raw []byte) []string {
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return []string{"ci.yml does not parse: " + err.Error()}
	}
	var problems []string
	for name, want := range callerTimeouts {
		job, ok := workflow.Jobs[name]
		if !ok {
			problems = append(problems, "ci.yml has no "+name+" job")
			continue
		}
		if job.Timeout != want {
			problems = append(problems, fmt.Sprintf("job %s timeout-minutes = %d, want %d", name, job.Timeout, want))
		}
	}
	if job, ok := workflow.Jobs["go-ci"]; ok && job.Timeout != 0 {
		problems = append(problems, fmt.Sprintf("job go-ci sets timeout-minutes = %d: reusable-call jobs reject it (actionlint syntax-check); the ceiling lives upstream", job.Timeout))
	}
	return problems
}

// TestCIWorkflowSetsJobTimeouts keeps a hung runner from holding a PR open
// forever: every caller job declares exactly its ceiling. The live ci.yml
// text must pass; each mutant must fail.
func TestCIWorkflowSetsJobTimeouts(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if problems := callerTimeoutProblems(raw); len(problems) > 0 {
		t.Errorf("ci.yml breaks the caller timeout contract:\n%s", strings.Join(problems, "\n"))
	}

	good := string(raw)
	for name, mutate := range map[string]func(string) string{
		// Timeout on the reusable call: GitHub rejects the workflow.
		"go-ci regains a timeout": func(s string) string {
			return strings.Replace(s, "needs: [changes]", "needs: [changes]\n    timeout-minutes: 60", 1)
		},
		// Bloated small job: a hung SARIF upload holds the run open.
		"gosec-sarif ceiling bloats": func(s string) string {
			idx := strings.Index(s, "gosec-sarif:")
			if idx < 0 {
				return s
			}
			tail := strings.Replace(s[idx:], "timeout-minutes: 5", "timeout-minutes: 120", 1)
			return s[:idx] + tail
		},
	} {
		mutated := mutate(good)
		if mutated == good {
			t.Fatalf("%s: the mutation did not change the workflow", name)
		}
		if len(callerTimeoutProblems([]byte(mutated))) == 0 {
			t.Errorf("%s: the gate accepted a timeout violation", name)
		}
	}
}

// TestCIWorkflowCancelsSupersededRuns keeps stale pushes from consuming
// runners: one concurrency group per PR (or ref) with cancellation.
func TestCIWorkflowCancelsSupersededRuns(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	if group := fmt.Sprint(workflow.Concurrency["group"]); !strings.Contains(group, "github.event.pull_request.number") {
		t.Errorf("concurrency group = %q, want one group per PR", group)
	}
	if cancel := fmt.Sprint(workflow.Concurrency["cancel-in-progress"]); cancel != "true" {
		t.Errorf("cancel-in-progress = %q, want true", cancel)
	}
}

// callerSecretProblems returns every way raw breaks the caller secret
// contract: no `secrets.*` anywhere (fork PRs run read-only), and the only
// token reference is the automatic github.token in scheduled-findings env
// (the one step needing a write token for the weekly findings issue).
func callerSecretProblems(raw string) []string {
	var problems []string
	for _, match := range secretUsePattern.FindAllStringSubmatch(raw, -1) {
		problems = append(problems, "caller uses configured secret "+match[1]+", want no secrets at all")
	}
	if n := strings.Count(raw, "github.token"); n != 1 {
		problems = append(problems, fmt.Sprintf("caller references github.token %d times, want exactly once (scheduled-findings env)", n))
	}
	findingsIdx := strings.Index(raw, "scheduled-findings:")
	tokenIdx := strings.Index(raw, "github.token")
	sarifIdx := strings.Index(raw, "gosec-sarif:")
	if findingsIdx < 0 || tokenIdx < 0 || sarifIdx < 0 {
		problems = append(problems, "caller lacks the scheduled-findings/gosec-sarif shape the secret contract assumes")
	} else if tokenIdx < findingsIdx || tokenIdx > sarifIdx {
		problems = append(problems, "github.token sits outside the scheduled-findings job: only that job may carry a token")
	}
	return problems
}

// TestCIWorkflowPRLeanesNeedNoConfiguredSecrets keeps fork PRs working: the
// only token any caller job may touch is the automatic github.token, used
// solely by the schedule-gated findings job, and the workflow holds only
// contents: read. The live ci.yml text must pass; each mutant must fail.
func TestCIWorkflowPRLeanesNeedNoConfiguredSecrets(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := readCIWorkflow(t)
	if got := workflow.Permissions["contents"]; got != "read" {
		t.Errorf("top-level contents permission = %q, want read", got)
	}
	if problems := callerSecretProblems(string(raw)); len(problems) > 0 {
		t.Errorf("ci.yml breaks the caller secret contract:\n%s", strings.Join(problems, "\n"))
	}

	good := string(raw)
	for name, mutate := range map[string]func(string) string{
		// Configured secret: fork PRs have no such secret and fail.
		"caller reads a configured secret": func(s string) string {
			return strings.Replace(s, "GH_TOKEN: ${{ github.token }}", "GH_TOKEN: ${{ secrets.CI_PAT }}", 1)
		},
		// Second token: another job now needs a write token on PRs.
		"gosec-sarif gains its own token": func(s string) string {
			return strings.Replace(s, "security-events: write", "security-events: write\n    env:\n      GH_TOKEN: ${{ github.token }}", 1)
		},
	} {
		mutated := mutate(good)
		if mutated == good {
			t.Fatalf("%s: the mutation did not change the workflow", name)
		}
		if len(callerSecretProblems(mutated)) == 0 {
			t.Errorf("%s: the gate accepted a secret violation", name)
		}
	}
}

// TestChangesDefinesFixtureAndMiscClasses keeps the skip matrix honest: the
// changes job must define the fixtures class (fixture-only PRs run test
// only) and the misc catch-all (unclassified files run the full Go lanes),
// with the some-with-excludes quantifier the misc negations need.
func TestChangesDefinesFixtureAndMiscClasses(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"fixtures:", "misc:", "predicate-quantifier: 'some-with-excludes'"} {
		if !strings.Contains(text, want) {
			t.Errorf("ci.yml changes job lacks %q", want)
		}
	}
	// Every fixtures entry must be negated in misc, or a fixture-only PR
	// also trips misc and pays for lint and race.
	miscIdx := strings.Index(text, "misc:")
	if miscIdx < 0 {
		t.Fatal("ci.yml has no misc class")
	}
	misc := text[miscIdx:]
	for _, entry := range []string{"manifest.yaml", "config.example.yaml", "**/*.json", "**/*.sh", "Dockerfile"} {
		if !strings.Contains(misc, "'!"+entry+"'") {
			t.Errorf("misc class does not negate %q: a narrow-lane PR would also run the full lanes", entry)
		}
	}
}

// callerGoCI mirrors the reusable-call job shape: job-level `uses:` plus
// `with:` inputs. workflowFile drops both, so the caller contract parses
// them separately without touching the release test struct.
type callerGoCI struct {
	Uses string         `yaml:"uses"`
	With map[string]any `yaml:"with"`
}

// readGoCICall returns the live go-ci reusable reference and its inputs.
func readGoCICall(t *testing.T) (string, map[string]any) {
	t.Helper()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var shaped struct {
		Jobs map[string]callerGoCI `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &shaped); err != nil {
		t.Fatal(err)
	}
	call, ok := shaped.Jobs["go-ci"]
	if !ok {
		t.Fatal("ci.yml has no go-ci job")
	}
	if call.With == nil {
		t.Fatal("go-ci job carries no with inputs")
	}
	return call.Uses, call.With
}

// requiredGoCIRunInputs are the lane selectors the caller must wire:
// dropping one silently un-runs its lane (reusable defaults decide then,
// not this repo's changes job).
var requiredGoCIRunInputs = []string{
	"run-image", "run-lint", "run-test", "run-race", "run-shell",
	"run-boundary", "run-govulncheck", "run-gosec", "run-actionlint",
}

// goCICallProblems returns every way a go-ci call breaks its contract: the
// reference must end at the pinned reusable SHA, every run-* selector input
// must be present, the coverage floor and gosec version must match this
// repo's measured baselines, and the image and boundary commands must carry
// their proof markers (digest pinning, the import-boundary test name).
func goCICallProblems(uses string, with map[string]any) []string {
	var problems []string
	if !strings.HasSuffix(uses, "@"+reusableSHA) {
		problems = append(problems, "go-ci uses "+uses+", want the reusable pinned at "+reusableSHA)
	}
	for _, input := range requiredGoCIRunInputs {
		if _, ok := with[input]; !ok {
			problems = append(problems, "go-ci call drops input "+input+" (its lane silently un-runs)")
		}
	}
	if got := fmt.Sprint(with["coverage-floor"]); got != "90" {
		problems = append(problems, fmt.Sprintf("go-ci coverage-floor = %q, want 90 (the measured floor)", got))
	}
	if got := fmt.Sprint(with["gosec-version"]); got != "v2.30.0" {
		problems = append(problems, fmt.Sprintf("go-ci gosec-version = %q, want v2.30.0 (this repo's measured 1-advisory baseline)", got))
	}
	imageCmd := fmt.Sprint(with["image-command"])
	for _, marker := range []string{"seed --dest", "digest-pinned"} {
		if !strings.Contains(imageCmd, marker) {
			problems = append(problems, "go-ci image-command lacks "+marker+" (the digest-pinning proof is gone)")
		}
	}
	if boundaryCmd := fmt.Sprint(with["boundary-command"]); !strings.Contains(boundaryCmd, "TestImportBoundaryRejectsCoreAndCrossCollectorImports") {
		problems = append(problems, "go-ci boundary-command never runs TestImportBoundaryRejectsCoreAndCrossCollectorImports")
	}
	return problems
}

// TestGoCICallWiresThisReposGates pins the thin-caller half of the reusable
// contract: the call stays on the pinned SHA, wires every lane selector,
// and keeps this repo's measured gates (floor 90, gosec v2.30.0, digest
// proof, import-boundary test). The live ci.yml text must pass; each
// mutant must fail.
func TestGoCICallWiresThisReposGates(t *testing.T) {
	t.Parallel()

	uses, with := readGoCICall(t)
	if problems := goCICallProblems(uses, with); len(problems) > 0 {
		t.Errorf("go-ci call breaks its contract:\n%s", strings.Join(problems, "\n"))
	}

	clone := func() map[string]any {
		out := make(map[string]any, len(with))
		for k, v := range with {
			out[k] = v
		}
		return out
	}
	for name, mutated := range map[string]struct {
		uses string
		with map[string]any
	}{
		// Floating reusable: PRs run whatever v2 moved to.
		"reusable floats on v2 tag": {uses: "eshu-hq/.github/.github/workflows/go-collector-ci.yml@v2", with: clone()},
		"reusable wrong SHA":        {uses: "eshu-hq/.github/.github/workflows/go-collector-ci.yml@0000000000000000000000000000000000000000", with: clone()},
	} {
		if len(goCICallProblems(mutated.uses, mutated.with)) == 0 {
			t.Errorf("%s: the gate accepted a bad reusable reference", name)
		}
	}
	withMutants := map[string]func(map[string]any){
		// Dropped selector: the shell lane silently un-runs on PRs.
		"dropped run-shell input": func(w map[string]any) { delete(w, "run-shell") },
		// Zeroed floor: coverage can collapse without failing CI.
		"coverage floor 0": func(w map[string]any) { w["coverage-floor"] = 0 },
		// Marker-less image command: digest pinning unproven, still green.
		"image command without markers": func(w map[string]any) { w["image-command"] = "docker build ." },
		// Generic boundary command: the import-boundary gate never runs.
		"boundary command without the test name": func(w map[string]any) { w["boundary-command"] = "go test ./..." },
	}
	for name, mutate := range withMutants {
		w := clone()
		mutate(w)
		if len(goCICallProblems(uses, w)) == 0 {
			t.Errorf("%s: the gate accepted a bad go-ci input", name)
		}
	}
}

func readCIWorkflow(t *testing.T) workflowFile {
	t.Helper()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.Jobs) == 0 {
		t.Fatal("ci.yml defines no jobs")
	}
	return workflow
}
