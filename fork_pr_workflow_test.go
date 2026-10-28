// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// forkSafetyProblems returns every way raw breaks the fork-PR contract: the
// workflow holds only contents:read, the gosec SARIF upload is skipped on
// pull_request (a skipped step keeps its lane green while a 403 code-scanning
// write would fail it), and every github.token issue step is schedule-gated
// so no PR lane executes a step that needs a write token.
func forkSafetyProblems(raw []byte) []string {
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return []string{"ci.yml does not parse: " + err.Error()}
	}
	var problems []string
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		problems = append(problems, "top-level permissions hold more than contents: read: fork PR lanes need contents:read alone")
	}
	gosec, ok := workflow.Jobs["gosec"]
	if !ok {
		return append(problems, "ci.yml has no gosec job")
	}
	uploadIdx := stepIndex(gosec.Steps, func(s workflowStep) bool { return strings.Contains(s.Uses, "upload-sarif") })
	if uploadIdx < 0 {
		problems = append(problems, "gosec job has no SARIF upload step")
	} else if !strings.Contains(gosec.Steps[uploadIdx].If, "github.event_name != 'pull_request'") {
		problems = append(problems, "gosec SARIF upload is not skipped on pull_request: fork PRs cannot write code-scanning results and the lane fails")
	}
	for _, name := range []string{"govulncheck", "gosec"} {
		job, ok := workflow.Jobs[name]
		if !ok {
			problems = append(problems, "ci.yml has no "+name+" job")
			continue
		}
		for _, step := range job.Steps {
			if !strings.Contains(step.Run, "gh issue create") {
				continue
			}
			for _, want := range []string{"failure()", "schedule"} {
				if !strings.Contains(step.If, want) {
					problems = append(problems, name+" issue step runs without "+want+": a PR lane would need a write token")
				}
			}
		}
	}
	for name, job := range workflow.Jobs {
		for scope, access := range job.Permissions {
			if access == "write" && scope != "issues" && scope != "security-events" {
				problems = append(problems, "job "+name+" grants "+scope+": "+access+": PR lanes need read scopes plus only the schedule-gated issue/SARIF writes")
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
		"SARIF upload runs on PRs": func(s string) string {
			return strings.Replace(s, "if: github.event_name != 'pull_request'\n        uses: github/codeql-action/upload-sarif", "if: always()\n        uses: github/codeql-action/upload-sarif", 1)
		},
		"workflow takes contents write": func(s string) string {
			return strings.Replace(s, "permissions:\n  contents: read", "permissions:\n  contents: write", 1)
		},
		"issue steps run on PRs": func(s string) string {
			return strings.ReplaceAll(s, "if: failure() && github.event_name == 'schedule'", "if: always()")
		},
		"job grants actions write": func(s string) string {
			return strings.Replace(s, "security-events: write", "security-events: write\n      actions: write", 1)
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
