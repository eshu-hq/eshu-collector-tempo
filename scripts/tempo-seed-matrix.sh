#!/usr/bin/env bash
# Seeded RED/GREEN matrix for the CI lanes (epic #6 acceptance).
#
# Local runner, not a CI gate: it seeds a defect, asserts the named lane
# turns red, removes the seed, and asserts the lane turns green again.
# It never compares the tree against a committed count or list.
#
# Usage: bash scripts/tempo-seed-matrix.sh
# Must leave the tree clean: every seed is removed after its check.
set -euo pipefail

cd "$(dirname "$0")/.."

TMP="$(mktemp -d)"
LOG="$TMP/last.log"
pass=0
fail=0

cleanup() {
	rm -f zz_seed_vet.go zz_seed_lint.go zz_seed_format.go \
		zz_seed_matrix_race_test.go zz_seed_cap.go
	git reset -q 2>/dev/null || true
	git checkout -q -- go.mod go.sum 2>/dev/null || true
	rm -rf "$TMP"
}
trap cleanup EXIT

# Refuse only on leftover seeds from an earlier run; the lane's own
# uncommitted diff (this script, the ci.yml reseed note) is expected.
# The end-of-run check diffs the tree against this snapshot instead.
if ls zz_seed_* >/dev/null 2>&1; then
	echo "FAIL: leftover seed files present; refusing to seed"
	ls zz_seed_*
	exit 1
fi
if ! git diff --exit-code -- go.mod go.sum >/dev/null 2>&1; then
	echo "FAIL: go.mod/go.sum already drifted; refusing to seed"
	exit 1
fi
SNAPSHOT="$TMP/status-before.txt"
git status --porcelain >"$SNAPSHOT"

ok() { # $1: check name
	echo "PASS: $1"
	pass=$((pass + 1))
}
bad() { # $1: check name
	echo "FAIL: $1"
	tail -n 20 "$LOG" 2>/dev/null || true
	fail=$((fail + 1))
}

# --- vet: seeded printf mismatch turns the vet step red ---
cat > zz_seed_vet.go <<'EOF'
package collector

import "fmt"

var _ = fmt.Sprintf("%d", "not-a-number")
EOF
if go vet ./... >"$LOG" 2>&1; then
	bad "vet RED on seeded printf mismatch (lane stayed green)"
elif grep -q "zz_seed_vet.go" "$LOG"; then
	ok "vet RED on seeded printf mismatch"
else
	bad "vet RED on seeded printf mismatch (no finding on the seed)"
fi
rm -f zz_seed_vet.go
if go vet ./... >"$LOG" 2>&1; then
	ok "vet GREEN after seed removal"
else
	bad "vet GREEN after seed removal"
fi

# --- lint: seeded strings.Replace with -1 trips staticcheck QF1004 ---
cat > zz_seed_lint.go <<'EOF'
package collector

import "strings"

var _ = strings.Replace("aaa", "a", "b", -1)
EOF
if golangci-lint run ./... >"$LOG" 2>&1; then
	bad "lint RED on seeded QF1004 (lane stayed green)"
elif grep -q "QF1004" "$LOG"; then
	ok "lint RED on seeded QF1004"
else
	bad "lint RED on seeded QF1004 (no QF1004 finding)"
fi
rm -f zz_seed_lint.go
if golangci-lint run ./... >"$LOG" 2>&1; then
	ok "lint GREEN after seed removal"
else
	bad "lint GREEN after seed removal"
fi

# --- format: seeded unformatted file trips the gofumpt diff check ---
printf 'package collector\n\nfunc zzSeedFormat() { x:=1; _=x }\n' > zz_seed_format.go
if golangci-lint fmt --diff ./... >"$LOG" 2>&1; then
	bad "format RED on seeded unformatted file (lane stayed green)"
elif grep -q "zz_seed_format.go" "$LOG"; then
	ok "format RED on seeded unformatted file"
else
	bad "format RED on seeded unformatted file (no diff on the seed)"
fi
rm -f zz_seed_format.go
if golangci-lint fmt --diff ./... >"$LOG" 2>&1; then
	ok "format GREEN after seed removal"
else
	bad "format GREEN after seed removal"
fi

# --- race: seeded data race fails the race lane ---
cat > zz_seed_matrix_race_test.go <<'EOF'
package collector

import "testing"

func TestSeedMatrixRace(t *testing.T) {
	x := 0
	done := make(chan bool)
	go func() { x = 1; done <- true }()
	_ = x
	<-done
}
EOF
if go test -race -count=1 -run TestSeedMatrixRace . >"$LOG" 2>&1; then
	bad "race RED on seeded data race (lane stayed green)"
elif grep -q "DATA RACE" "$LOG"; then
	ok "race RED on seeded data race"
else
	bad "race RED on seeded data race (no race reported)"
fi
rm -f zz_seed_matrix_race_test.go
# GREEN runs the full lane and writes the profile the floor seeds reuse.
if go test ./... -race -count=1 -covermode=atomic -coverprofile="$TMP/coverage.out" >"$LOG" 2>&1; then
	ok "race GREEN after seed removal"
else
	bad "race GREEN after seed removal"
fi

# --- tidy: seeded requirement drift trips go mod tidy -diff ---
go mod edit -require=gopkg.in/yaml.v3@v3.0.0
if go mod tidy -diff >"$LOG" 2>&1; then
	bad "tidy RED on seeded drift (lane stayed green)"
else
	ok "tidy RED on seeded drift"
fi
git checkout -q -- go.mod go.sum
if go mod tidy -diff >"$LOG" 2>&1 && git diff --exit-code -- go.mod go.sum >"$LOG" 2>&1; then
	ok "tidy GREEN after seed removal"
else
	bad "tidy GREEN after seed removal"
fi

# --- 500-line cap: seeded over-cap file trips the wc -l step ---
# Derive the threshold live from the lane the way the floor section derives
# COVERAGE_FLOOR, so a lane edit propagates to the matrix instead of being
# silently shadowed by a hardcoded copy.
# shellcheck disable=SC2016 # $f/$n are literal grep text (fixed-string match), not expansions.
cap=$(grep -F 'echo "$f: $n lines"' .github/workflows/ci.yml | sed -n 's/.*-gt \([0-9][0-9]*\).*/\1/p')
if [ -z "$cap" ] || [ "$(printf '%s\n' "$cap" | wc -l)" -ne 1 ]; then
	echo "FAIL: cap RED (no single live threshold in ci.yml)" | tee -a "$LOG"
	fail=$((fail + 1))
else
{
	echo "package collector"
	echo ""
	echo "// zzSeedCapPad pads this seed past the live cap ($cap lines)."
	for i in $(seq 1 "$((cap - 1))"); do echo "var _ = $i"; done
} > zz_seed_cap.go
# Intent-to-add so the lane's own `git ls-files '*.go'` loop sees the seed.
git add -N zz_seed_cap.go
over=0
for f in $(git ls-files '*.go'); do
	n=$(wc -l <"$f")
	if [ "$n" -gt "$cap" ] && [ "$f" = "zz_seed_cap.go" ]; then over=1; fi
done
if [ "$over" -eq 1 ]; then
	ok "cap RED on seeded over-cap file (live cap $cap)"
else
	bad "cap RED on seeded over-cap file (loop did not flag the seed)"
fi
git reset -q -- zz_seed_cap.go
rm -f zz_seed_cap.go
over=0
for f in $(git ls-files '*.go'); do
	n=$(wc -l <"$f")
	if [ "$n" -gt "$cap" ]; then echo "$f: $n lines"; over=1; fi
done >"$LOG" 2>&1
if [ "$over" -eq 0 ]; then
	ok "cap GREEN after seed removal"
else
	bad "cap GREEN after seed removal"
fi
fi

# --- coverage floor: an unreachable floor fails, the live floor passes ---
total=$(go tool cover -func="$TMP/coverage.out" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')
echo "measured total=${total}%"
if awk -v t="$total" -v f="99" 'BEGIN { if (t + 0 < f + 0) { exit 1 } }'; then
	bad "floor RED at 99 (measured $total)"
else
	ok "floor RED at 99 (measured $total)"
fi
floor=$(sed -n 's/.*COVERAGE_FLOOR: "\([0-9.]*\)".*/\1/p' .github/workflows/ci.yml | head -n 1)
if [ -z "$floor" ]; then
	echo "FAIL: floor GREEN (no COVERAGE_FLOOR in live ci.yml)" | tee -a "$LOG"
	fail=$((fail + 1))
elif awk -v t="$total" -v f="$floor" 'BEGIN { if (t + 0 < f + 0) { print "FAIL: coverage below floor"; exit 1 } }'; then
	ok "floor GREEN at $floor (measured $total)"
else
	bad "floor GREEN at $floor (measured $total)"
fi

# --- ci-complete: the live aggregator fails failed/cancelled, passes skips ---
AGG="$TMP/aggregate.py"
awk '/python3 - <<.EOF./ { flag=1; next } /^[[:space:]]*EOF$/ { if (flag) { flag=0; exit } } flag { sub(/^          /, ""); print }' \
	.github/workflows/ci.yml >"$AGG"
if [ ! -s "$AGG" ]; then
	echo "FAIL: ci-complete seeds (aggregator not found in live ci.yml)" | tee -a "$LOG"
	fail=$((fail + 3))
else
	red_failed='{"changes":{"result":"success"},"lint":{"result":"failure"},"test":{"result":"success"},"race":{"result":"skipped"},"shell":{"result":"skipped"},"image":{"result":"skipped"},"boundary":{"result":"success"},"govulncheck":{"result":"skipped"},"gosec":{"result":"skipped"},"actionlint":{"result":"success"}}'
	if NEEDS_JSON="$red_failed" python3 "$AGG" >"$LOG" 2>&1; then
		bad "ci-complete RED on failed lane (aggregator stayed green)"
	else
		ok "ci-complete RED on failed lane"
	fi
	red_cancelled='{"changes":{"result":"success"},"lint":{"result":"cancelled"},"test":{"result":"success"},"race":{"result":"skipped"},"shell":{"result":"skipped"},"image":{"result":"skipped"},"boundary":{"result":"success"},"govulncheck":{"result":"skipped"},"gosec":{"result":"skipped"},"actionlint":{"result":"success"}}'
	if NEEDS_JSON="$red_cancelled" python3 "$AGG" >"$LOG" 2>&1; then
		bad "ci-complete RED on cancelled lane (aggregator stayed green)"
	else
		ok "ci-complete RED on cancelled lane"
	fi
	green_skips='{"changes":{"result":"success"},"lint":{"result":"success"},"test":{"result":"success"},"race":{"result":"skipped"},"shell":{"result":"skipped"},"image":{"result":"skipped"},"boundary":{"result":"success"},"govulncheck":{"result":"skipped"},"gosec":{"result":"success"},"actionlint":{"result":"success"}}'
	if NEEDS_JSON="$green_skips" python3 "$AGG" >"$LOG" 2>&1; then
		ok "ci-complete GREEN with skips"
	else
		bad "ci-complete GREEN with skips"
	fi
fi

# --- reachability: aggregator and issue-step contract tests on live ci.yml ---
if go test -count=1 -run 'TestCICompleteAggregatesPerJobResults|TestGovulncheckIssueStepIsReachable|TestGosecIssueStepIsReachable' . >"$LOG" 2>&1; then
	ok "reachability tests GREEN on live ci.yml"
else
	bad "reachability tests GREEN on live ci.yml"
fi

echo "seed matrix: pass=$pass fail=$fail"
if ! diff -u "$SNAPSHOT" <(git status --porcelain) >"$TMP/status-diff.txt" 2>&1; then
	echo "FAIL: tree changed by the matrix run:"
	cat "$TMP/status-diff.txt"
	exit 1
fi
[ "$fail" -eq 0 ]
