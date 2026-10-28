// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"
)

// issueStepProblems returns every way a scheduled findings issue step
// breaks its create-or-comment contract: the step must carry the
// failure() plus schedule gate (fork-PR safety), and its body must look
// up the open findings issue first (gh issue list) and comment on it
// (gh issue comment), creating (gh issue create) only when none is
// open — otherwise every weekly run files a duplicate issue.
// isShellWordByte reports whether b can sit inside a shell word, so the
// branch-token scan does not match "fi" inside "if" or any other word.
func isShellWordByte(b byte) bool {
	return b == '_' || '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

func issueStepProblems(run string, issue workflowStep) []string {
	var problems []string
	if !strings.Contains(issue.If, "failure()") {
		problems = append(problems, "findings issue step if lacks failure()")
	}
	// Anchor the schedule polarity: a bare "schedule" substring also
	// matches an inverted != gate that runs on every PR instead of only
	// on the weekly schedule.
	if !strings.Contains(issue.If, "== 'schedule'") {
		problems = append(problems, "findings issue step if lacks == 'schedule' polarity")
	}
	if strings.Contains(issue.If, "!=") {
		problems = append(problems, "findings issue step if carries !=: an inverted gate fires on PRs, not on schedule")
	}
	// Require the branch structure, not co-occurrence: the list lookup
	// must precede the comment/create arms inside an if/else, otherwise
	// an echo-decoy body naming the commands passes without branching.
	listIdx := strings.Index(run, "gh issue list")
	commentIdx := strings.Index(run, "gh issue comment")
	createIdx := strings.Index(run, "gh issue create")
	if listIdx < 0 {
		problems = append(problems, "findings issue step body lacks gh issue list (weekly runs would duplicate issues)")
	}
	if commentIdx < 0 {
		problems = append(problems, "findings issue step body lacks gh issue comment (weekly runs would duplicate issues)")
	}
	if createIdx < 0 {
		problems = append(problems, "findings issue step body lacks gh issue create (weekly runs would duplicate issues)")
	}
	if listIdx >= 0 && commentIdx >= 0 && createIdx >= 0 {
		// Require ordered shell-branch tokens, not word co-occurrence:
		// a branchless body can mention the words "if" and "else"
		// without branching. The list lookup must precede the branch.
		ifPos := strings.Index(run, "if ")
		thenPos, elsePos := -1, -1
		if ifPos >= 0 {
			if i := strings.Index(run[ifPos+len("if "):], "then"); i >= 0 {
				thenPos = ifPos + len("if ") + i
				if j := strings.Index(run[thenPos+len("then"):], "else"); j >= 0 {
					elsePos = thenPos + len("then") + j
				}
			}
		}
		closed := false
		if elsePos >= 0 {
			rest := run[elsePos+len("else"):]
			for j := 0; j+2 <= len(rest); j++ {
				if rest[j:j+2] != "fi" {
					continue
				}
				prevOK := j == 0 || !isShellWordByte(rest[j-1])
				nextOK := j+2 >= len(rest) || !isShellWordByte(rest[j+2])
				if prevOK && nextOK {
					closed = true
					break
				}
			}
		}
		switch {
		case ifPos < 0 || thenPos < 0 || elsePos < 0 || !closed:
			problems = append(problems, "findings issue step body has no if/then/else/fi branch: a decoy naming the commands passes without branching")
		case listIdx >= ifPos || listIdx >= commentIdx || listIdx >= createIdx:
			problems = append(problems, "findings issue step body lists no open issue before the branch arms")
		}
	}
	return problems
}

// TestScheduledIssueStepsCreateOrComment pins the weekly-schedule
// notification shape for both scanning lanes: each findings issue step
// exists, stays gated on failure() plus schedule, and comments on the
// open findings issue instead of filing a duplicate every Monday. The
// live ci.yml text must pass; a create-only mutant and an ungated
// mutant must fail (mirrors TestCICompleteAggregatesPerJobResults).
func TestScheduledIssueStepsCreateOrComment(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	for _, jobName := range []string{"govulncheck", "gosec"} {
		job, ok := workflow.Jobs[jobName]
		if !ok {
			t.Fatalf("ci.yml has no %s job", jobName)
		}
		issueIdx := stepIndex(job.Steps, func(s workflowStep) bool { return strings.Contains(s.Run, "gh issue create") })
		if issueIdx < 0 {
			t.Fatalf("%s job has no findings issue step", jobName)
		}
		if problems := issueStepProblems(job.Steps[issueIdx].Run, job.Steps[issueIdx]); len(problems) > 0 {
			t.Errorf("%s findings issue step breaks its contract:\n%s", jobName, strings.Join(problems, "\n"))
		}
	}

	issue := workflowStep{If: "failure() && github.event_name == 'schedule'"}
	// Create-only body: files a duplicate issue every scheduled run.
	if len(issueStepProblems(`gh issue create --title "$title" --body "$body"`, issue)) == 0 {
		t.Errorf("create-only body: the gate accepted a step that duplicates issues weekly")
	}
	// Ungated step: would fire (and need a token) on fork PRs.
	ungated := workflowStep{}
	ungatedRun := `open=$(gh issue list --state open); if [ -n "$open" ]; then gh issue comment "$open"; else gh issue create; fi`
	if len(issueStepProblems(ungatedRun, ungated)) == 0 {
		t.Errorf("ungated issue step: the gate accepted a step without failure()/schedule")
	}
	// Inverted gate: matches a bare "schedule" substring but fires on
	// every PR instead of only on the weekly schedule.
	inverted := workflowStep{If: "failure() && github.event_name != 'schedule'"}
	if len(issueStepProblems(ungatedRun, inverted)) == 0 {
		t.Errorf("inverted gate: the gate accepted failure() && event != schedule")
	}
	// Echo decoy: names every command but branches on nothing, so the
	// unconditional create files a duplicate issue every scheduled run.
	decoyRun := `echo "gh issue list gh issue comment (audited, no action)"; gh issue create --title "$title" --body "$body"`
	if len(issueStepProblems(decoyRun, issue)) == 0 {
		t.Errorf("echo decoy: the gate accepted a branchless body that duplicates issues weekly")
	}
	// Word echo: mentions if/else without any branch tokens that form a
	// statement, so the unconditional arms still duplicate issues weekly.
	wordRun := `echo "if open else create (audited, no action)"; gh issue list a; gh issue comment b; gh issue create c`
	if len(issueStepProblems(wordRun, issue)) == 0 {
		t.Errorf("word echo: the gate accepted branchless if/else words")
	}
}
