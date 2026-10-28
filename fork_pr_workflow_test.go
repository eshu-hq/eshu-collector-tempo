// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// prSkipGate is the exact job-level gate that keeps the SARIF upload off
// pull requests: a skipped job keeps its lane green, while a 403
// code-scanning write would fail it. Compare the exact form, not a
// substring: a tautological condition (|| true) runs on PRs.
const prSkipGate = "github.event_name != 'pull_request'"

// findingsGate is the exact job-level gate for the weekly findings issue:
// it fires only when the reusable failed on the schedule event, so no PR
// lane executes a step that needs a write token.
const findingsGate = "failure() && github.event_name == 'schedule'"

// forkSafetyProblems returns every way raw breaks the fork-PR contract
// against the caller jobs only: the workflow holds exactly contents:read,
// the gosec-sarif job is skipped on pull_request with continue-on-error,
// the scheduled-findings job carries issues:write behind its
// failure()+schedule gate, and no other job grants a write scope.
func forkSafetyProblems(raw []byte) []string {
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return []string{"ci.yml does not parse: " + err.Error()}
	}
	var problems []string
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		problems = append(problems, "top-level permissions hold more than contents: read: fork PR lanes need contents:read alone")
	}
	sarif, ok := workflow.Jobs["gosec-sarif"]
	if !ok {
		problems = append(problems, "ci.yml has no gosec-sarif job")
	} else {
		if strings.TrimSpace(sarif.If) != prSkipGate {
			problems = append(problems, "gosec-sarif job is not skipped on pull_request: fork PRs cannot write code-scanning results and the lane fails")
		}
		if idx := stepIndex(sarif.Steps, func(s workflowStep) bool { return strings.Contains(s.Uses, "upload-sarif") }); idx < 0 {
			problems = append(problems, "gosec-sarif job has no SARIF upload step")
		} else if !sarif.Steps[idx].ContinueOnError {
			problems = append(problems, "gosec-sarif upload step lacks continue-on-error: a 403 would fail the lane instead of staying advisory")
		}
	}
	findings, ok := workflow.Jobs["scheduled-findings"]
	if !ok {
		problems = append(problems, "ci.yml has no scheduled-findings job")
	} else {
		if strings.TrimSpace(findings.If) != findingsGate {
			problems = append(problems, "scheduled-findings job is not failure()-gated on schedule: a PR lane would need a write token")
		}
		if findings.Permissions["issues"] != "write" {
			problems = append(problems, "scheduled-findings job lacks issues: write: it cannot file the weekly findings issue")
		}
		// Scan the caller jobs, not just the two gated ones: an
		// issue-creating step in any other job runs on PRs with a
		// read-only token.
		for name, job := range workflow.Jobs {
			if name == "scheduled-findings" {
				continue
			}
			for _, step := range job.Steps {
				if strings.Contains(step.Run, "gh issue create") {
					problems = append(problems, "job "+name+" creates issues outside the schedule-gated findings job: a PR lane would need a write token")
				}
			}
		}
	}
	for name, job := range workflow.Jobs {
		for scope, access := range job.Permissions {
			if access != "write" {
				continue
			}
			switch {
			case name == "scheduled-findings" && scope == "issues":
				// The schedule-gated findings write.
			case name == "gosec-sarif" && scope == "security-events":
				// The PR-skipped SARIF upload.
			default:
				problems = append(problems, "job "+name+" grants "+scope+": "+access+": PR lanes need read scopes plus only the gated issue/SARIF writes")
			}
		}
	}
	return problems
}

// TestCIWorkflowForkPRSafe pins the fork-safe shape: required lanes run on a
// read-only token with no secrets, and the SARIF upload stays skipped on PRs
// without failing its lane. The live text must pass; each mutant must fail.
func TestCIWorkflowForkPRSafe(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if problems := forkSafetyProblems(raw); len(problems) > 0 {
		t.Errorf("ci.yml breaks the fork-PR contract:\n%s", strings.Join(problems, "\n"))
	}

	good := string(raw)
	for name, mutate := range map[string]func(string) string{
		"SARIF job runs on PRs": func(s string) string {
			return strings.Replace(s, "if: "+prSkipGate, "if: always()", 1)
		},
		"workflow takes contents write": func(s string) string {
			return strings.Replace(s, "permissions:\n  contents: read", "permissions:\n  contents: write", 1)
		},
		"findings job runs on PRs": func(s string) string {
			return strings.Replace(s, "if: "+findingsGate, "if: always()", 1)
		},
		"SARIF job tautology runs on PRs": func(s string) string {
			return strings.Replace(s, "if: "+prSkipGate, "if: "+prSkipGate+" || true", 1)
		},
		"findings job tautology runs on PRs": func(s string) string {
			return strings.Replace(s, "if: "+findingsGate, "if: failure() || github.event_name == 'schedule' || true", 1)
		},
		"job grants actions write": func(s string) string {
			return strings.Replace(s, "security-events: write", "security-events: write\n      actions: write", 1)
		},
		"SARIF upload loses continue-on-error": func(s string) string {
			return strings.Replace(s, "        continue-on-error: true\n", "", 1)
		},
	} {
		mutated := mutate(good)
		if mutated == good {
			t.Fatalf("%s: the mutation did not change the workflow", name)
		}
		if len(forkSafetyProblems([]byte(mutated))) == 0 {
			t.Errorf("%s: the gate accepted a fork-unsafe workflow", name)
		}
	}
}
