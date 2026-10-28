// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"
)

// isShellWordByte reports whether b can sit inside a shell word, so the
// branch-token scan does not match "fi" inside "if" or any other word.
func isShellWordByte(b byte) bool {
	return b == '_' || '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

// indexShellWord finds word in s at or after from, matching only with
// word boundaries on both sides, so "then" never matches "authenticate"
// and "else" never matches "elsewhere". Returns -1 when absent.
func indexShellWord(s, word string, from int) int {
	for i := from; i+len(word) <= len(s); i++ {
		if s[i:i+len(word)] != word {
			continue
		}
		if i > 0 && isShellWordByte(s[i-1]) {
			continue
		}
		if end := i + len(word); end < len(s) && isShellWordByte(s[end]) {
			continue
		}
		return i
	}
	return -1
}

// issueStepProblems returns every way a scheduled findings issue step
// breaks its create-or-comment contract: the step must carry the
// failure() plus schedule gate (fork-PR safety), and its body must look
// up the open findings issue first (gh issue list) and comment on it
// (gh issue comment), creating (gh issue create) only when none is
// open — otherwise every weekly run files a duplicate issue.
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
		// Require ordered shell-branch tokens, not word co-occurrence,
		// with the comment/create arms pinned inside the branch: a
		// branchless body can carry the tokens anywhere (including a
		// trailing echo or comment) without branching. The list lookup
		// must precede the branch.
		ifPos := indexShellWord(run, "if", 0)
		thenPos, elsePos, fiPos := -1, -1, -1
		if ifPos >= 0 {
			thenPos = indexShellWord(run, "then", ifPos+len("if"))
			if thenPos >= 0 {
				elsePos = indexShellWord(run, "else", thenPos+len("then"))
				if elsePos >= 0 {
					if fi := indexShellWord(run, "fi", elsePos+len("else")); fi >= 0 {
						fiPos = fi
					}
				}
			}
		}
		listFirst := listIdx < ifPos
		commentInside := thenPos < commentIdx && commentIdx < fiPos+len("fi")
		createInside := thenPos < createIdx && createIdx < fiPos+len("fi")
		switch {
		case ifPos < 0 || thenPos < 0 || elsePos < 0 || fiPos < 0:
			problems = append(problems, "findings issue step body has no if/then/else/fi branch: a decoy naming the commands passes without branching")
		case !listFirst || !commentInside || !createInside:
			problems = append(problems, "findings issue step body does not run its arms inside the branch: unconditional arms duplicate issues weekly")
		}
	}
	return problems
}

// TestScheduledIssueStepsCreateOrComment pins the weekly-schedule
// notification shape for the single caller-side scheduled-findings job
// (the reusable owns the scanning lanes; it cannot file the issue): the
// findings step exists, the job stays gated on failure() plus schedule,
// and the step comments on the open findings issue instead of filing a
// duplicate every Monday. The live ci.yml text must pass; a create-only
// mutant and an ungated mutant must fail.
func TestScheduledIssueStepsCreateOrComment(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	job, ok := workflow.Jobs["scheduled-findings"]
	if !ok {
		t.Fatal("ci.yml has no scheduled-findings job")
	}
	issueIdx := stepIndex(job.Steps, func(s workflowStep) bool { return strings.Contains(s.Run, "gh issue create") })
	if issueIdx < 0 {
		t.Fatal("scheduled-findings job has no findings issue step")
	}
	// The gate lives on the job (one step, one job); the body check reads
	// the step. Polarity, branch structure, and ordering still apply.
	gate := workflowStep{If: job.If}
	if problems := issueStepProblems(job.Steps[issueIdx].Run, gate); len(problems) > 0 {
		t.Errorf("scheduled-findings issue step breaks its contract:\n%s", strings.Join(problems, "\n"))
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
	// Trailing echo: carries every branch token after unconditional arms,
	// so ordered co-occurrence alone still duplicates issues weekly.
	trailingRun := `gh issue list a; gh issue comment b; gh issue create c; echo "if x then y else z fi"`
	if len(issueStepProblems(trailingRun, issue)) == 0 {
		t.Errorf("trailing echo: the gate accepted branch tokens outside the arms")
	}
}
