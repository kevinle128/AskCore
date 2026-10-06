#!/usr/bin/env bash
# Conformance matrix check.
#
# Usage:
#   check-conformance-matrix.sh --dry-run   parse the matrix, check its structure, list rows per phase
#   check-conformance-matrix.sh NN          check and run the Go tests of the rows of phase NN
#   check-conformance-matrix.sh all         the same for every phase
#
# Structure checks (always): 8 cells per row, no empty cell, unique IDs, status in
# {follow, diverge-deliberate, Ask-new, N/A}, a reason for every non-follow row,
# N/A rows carry no test, assertion or phase, other rows carry all three, and the
# "Assertion review" list of each phase equals the set of its non-N/A rows.
#
# Test checks (phase NN or all): every named Go test must be listed by `go test -list`
# and must pass. The check fails while a row of the phase is not ticked in the
# "Assertion review" list.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"
matrix="$here/conformance-matrix.md"
mode="${1:-}"

if [[ -z "$mode" ]]; then
  echo "usage: $0 --dry-run | NN | all" >&2
  exit 2
fi
if [[ "$mode" != "--dry-run" && "$mode" != "all" && ! "$mode" =~ ^[0-9]{2}$ ]]; then
  echo "bad argument: $mode (expected --dry-run, a two-digit phase, or all)" >&2
  exit 2
fi
if [[ ! -f "$matrix" ]]; then
  echo "missing matrix: $matrix" >&2
  exit 2
fi

parse_out="$(mktemp)"
trap 'rm -f "$parse_out"' EXIT

# The parser prints one line per phase to be tested: "PHASE<TAB>ROW<TAB>NAME,NAME,...".
# Structure errors and the unticked list go to stderr and set the exit code.
python3 - "$matrix" "$mode" >"$parse_out" <<'PY'
import re
import sys
from collections import defaultdict

matrix, mode = sys.argv[1], sys.argv[2]
STATUSES = {"follow", "diverge-deliberate", "Ask-new", "N/A"}
errors = []

text = open(matrix, encoding="utf-8").read().split("\n")
rows = []
for n, line in enumerate(text, 1):
    if not line.startswith("| ") or line.startswith("| #"):
        continue
    cells = [c.strip() for c in re.split(r"(?<!\\)\|", line.strip())[1:-1]]
    if len(cells) == 3:  # vocabulary table
        continue
    if len(cells) != 8:
        errors.append(f"line {n}: {len(cells)} cells, expected 8")
        continue
    rows.append((n, cells))

seen = {}
by_phase = defaultdict(list)
for n, c in rows:
    rid, _beh, _src, status, reason, test, assertion, phase = c
    if any(x == "" for x in c):
        errors.append(f"row {rid} (line {n}): empty cell")
    if rid in seen:
        errors.append(f"row {rid} (line {n}): duplicate of line {seen[rid]}")
    seen[rid] = n
    if status not in STATUSES:
        errors.append(f"row {rid}: bad status {status!r}")
        continue
    if status != "follow" and reason in ("", "-"):
        errors.append(f"row {rid}: status {status} needs a reason")
    if status == "N/A":
        if test != "-" or assertion != "-" or phase != "-":
            errors.append(f"row {rid}: N/A row must have '-' in test, assertion and phase")
    else:
        if test == "-" or assertion == "-" or phase == "-":
            errors.append(f"row {rid}: {status} row needs test, assertion and phase")
        elif not re.fullmatch(r"\d{2}", phase):
            errors.append(f"row {rid}: bad phase {phase!r}")
        else:
            by_phase[phase].append((rid, test))

# Assertion review list.
review = {}
ticked = defaultdict(set)
in_review = False
for line in text:
    if line.startswith("## Assertion review"):
        in_review = True
        continue
    if in_review and line.startswith("## "):
        break
    m = re.match(r"- Phase (\d{2}):(.*)", line) if in_review else None
    if not m:
        continue
    ph = m.group(1)
    ids = []
    for box, rid in re.findall(r"\[([ xX])\]\s*(\S+)", m.group(2)):
        ids.append(rid)
        if box in "xX":
            ticked[ph].add(rid)
    if len(ids) != len(set(ids)):
        errors.append(f"review list of phase {ph} has a duplicate ID")
    review[ph] = set(ids)

for ph in sorted(set(by_phase) | set(review)):
    want = {rid for rid, _ in by_phase.get(ph, [])}
    got = review.get(ph, set())
    if want != got:
        errors.append(
            f"phase {ph}: review list differs from non-N/A rows: "
            f"missing {sorted(want - got)} extra {sorted(got - want)}"
        )

print(f"rows: {len(rows)}", file=sys.stderr)
counts = defaultdict(int)
for _, c in rows:
    counts[c[3]] += 1
print("status: " + ", ".join(f"{k}={counts[k]}" for k in sorted(counts)), file=sys.stderr)
for ph in sorted(by_phase):
    print(
        f"phase {ph}: {len(by_phase[ph])} rows, "
        f"{len(ticked[ph] & {r for r, _ in by_phase[ph]})} ticked",
        file=sys.stderr,
    )


def strip_parens(s):
    out, depth = [], 0
    for ch in s:
        if ch == "(":
            depth += 1
        elif ch == ")":
            depth = max(0, depth - 1)
        elif depth == 0:
            out.append(ch)
    return "".join(out)


if mode != "--dry-run":
    phases = sorted(by_phase) if mode == "all" else [mode]
    if mode != "all" and mode not in by_phase:
        print(f"phase {mode}: no testable rows", file=sys.stderr)
    for ph in phases:
        for rid, test in by_phase.get(ph, []):
            if rid not in ticked[ph]:
                errors.append(f"phase {ph} row {rid}: assertion not hand-reviewed (unticked)")
            plain = strip_parens(test.replace("`", ""))
            if plain.strip().startswith("see row"):
                continue
            names = []
            for part in plain.split(";"):
                part = part.strip()
                if part.startswith("see row"):
                    continue
                names += re.findall(r"Test\w+", part)
            if not names:
                errors.append(f"phase {ph} row {rid}: no Go test name in {test!r}")
                continue
            print(f"{ph}\t{rid}\t{','.join(dict.fromkeys(names))}")

if errors:
    for e in errors:
        print("ERROR: " + e, file=sys.stderr)
    sys.exit(1)
PY

if [[ "$mode" == "--dry-run" ]]; then
  echo "dry run: matrix structure and review lists are consistent"
  exit 0
fi

if [[ ! -s "$parse_out" ]]; then
  echo "no Go tests to check for phase $mode"
  exit 0
fi

cd "$root"
listing="$(mktemp)"
trap 'rm -f "$parse_out" "$listing"' EXIT
# "pkg<TAB>TestName" for every test in the module. `go test -list` prints the
# test names of a package before its "ok pkg" line, so the names are held until
# that line arrives.
list_out="$(mktemp)"
trap 'rm -f "$parse_out" "$listing" "$list_out"' EXIT
if ! go test -list '.*' ./... >"$list_out" 2>"$list_out.err"; then
  echo "go test -list failed; fix the build first:" >&2
  cat "$list_out.err" >&2
  rm -f "$list_out.err"
  exit 1
fi
rm -f "$list_out.err"
awk '
  /^(ok|FAIL|\?)[[:space:]]/ {
    for (i = 0; i < n; i++) print $2 "\t" names[i]
    n = 0
    next
  }
  /^(Test|Example|Benchmark|Fuzz)/ { names[n++] = $1 }
' "$list_out" >"$listing"

# Map every named test to its package; report missing names; print "pkg<TAB>regex".
plan_out="$(mktemp)"
trap 'rm -f "$parse_out" "$listing" "$plan_out"' EXIT
python3 - "$parse_out" "$listing" >"$plan_out" <<'PY'
import sys
from collections import defaultdict

parse_out, listing = sys.argv[1], sys.argv[2]
where = defaultdict(set)
for line in open(listing, encoding="utf-8"):
    pkg, name = line.rstrip("\n").split("\t")
    where[name].add(pkg)
per_pkg = defaultdict(list)
missing = False
for line in open(parse_out, encoding="utf-8"):
    ph, rid, names = line.rstrip("\n").split("\t")
    for name in names.split(","):
        if name not in where:
            print(f"MISSING: phase {ph} row {rid}: test {name} does not exist", file=sys.stderr)
            missing = True
            continue
        for pkg in where[name]:
            if name not in per_pkg[pkg]:
                per_pkg[pkg].append(name)
if missing:
    sys.exit(1)
for pkg in sorted(per_pkg):
    print(pkg + "\t" + "|".join(per_pkg[pkg]))
PY

fail=0
while IFS=$'\t' read -r pkg regex; do
  echo "== go test $pkg -run ^($regex)\$"
  if ! go test -count=1 -run "^($regex)\$" "$pkg"; then
    fail=1
  fi
done <"$plan_out"
exit $fail
