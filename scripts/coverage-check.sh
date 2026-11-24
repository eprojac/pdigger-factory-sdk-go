#!/bin/sh
# coverage-check.sh [cover.out]: fails unless every statement of the profile is covered. It counts exactly instead of
# reading the rounded total of go tool cover -func, which prints 100.0 % with one statement in 3500 uncovered: a block
# that several test binaries report (-coverpkg=./...) is covered when any of them ran it, and the check needs zero
# uncovered statements. When it fails it lists the uncovered blocks and the functions below 100 %. Nothing is excluded
# from coverage (see TESTING.md).
set -eu
profile=${1:-cover.out}
counts=$(awk '
  NR == 1 && /^mode:/ { next }
  NF == 3 {
    stmts[$1] = $2
    if ($3 > 0) covered[$1] = 1
  }
  END {
    total = 0; missed = 0
    for (b in stmts) {
      total += stmts[b]
      if (!(b in covered)) { missed += stmts[b]; print "uncovered " b " (" stmts[b] " statements)" | "sort" }
    }
    close("sort")
    print "total", total, missed
  }' "$profile")
set -- $(printf '%s\n' "$counts" | awk '$1 == "total" { print $2, $3 }')
total=$1
missed=$2
if [ "$total" -eq 0 ]; then
  echo "coverage-check: $profile has no statements"
  exit 1
fi
echo "statement coverage: $((total - missed)) of $total statements covered ($missed uncovered)"
if [ "$missed" -ne 0 ]; then
  printf '%s\n' "$counts" | grep '^uncovered '
  echo "functions below 100 %:"
  go tool cover -func="$profile" | awk '!/^total:/ && $NF != "100.0%"'
  exit 1
fi
echo "statement coverage: 100 %"
