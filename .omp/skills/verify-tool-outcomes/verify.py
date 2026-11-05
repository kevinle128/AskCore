#!/usr/bin/env python3
"""Model-check the tool batch of internal/agent and check that the model is in sync.

  python3 .omp/skills/verify-tool-outcomes/verify.py            # drift check + TLC
  python3 .omp/skills/verify-tool-outcomes/verify.py mutants    # every property fails on its break

Exit codes: 0 pass and in sync, 1 a property fails, 2 setup error,
3 the model passes but covered code changed since the sync commit.
"""
import hashlib
import os
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.request

SKILL_DIR = os.path.dirname(os.path.abspath(__file__))
MODEL_DIR = os.path.join(SKILL_DIR, "model")
REPO = subprocess.run(["git", "-C", SKILL_DIR, "rev-parse", "--show-toplevel"],
                      capture_output=True, text=True, check=True).stdout.strip()

TLA_VERSION = "1.7.4"
TLA_URL = f"https://github.com/tlaplus/tlaplus/releases/download/v{TLA_VERSION}/tla2tools.jar"
TLA_SHA256 = "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88"

# Each break mirrors one edit to the Go code: (name, Go edit, model text, replacement,
# properties allowed to report it). The model must fail on every one.
MUTANTS = [
    ("exclusive call starts while other bodies run",
     "tool_coordinator.go: `if !s.exclusive || (active == 0 && committed == next)` loses its wait",
     "~isExcl \\/ (active = 0 /\\ committed = next) ->", "TRUE ->",
     {"ExclusiveAlone", "ExclusiveBarrier"}),
    ("exclusive flag not raised when an exclusive body starts",
     "tool_coordinator.go: `exclusive = s.exclusive` removed from the start block",
     "exclFlag' = isExcl", "exclFlag' = FALSE",
     {"ExclusiveAlone", "ExclusiveBarrier"}),
    ("pool admits one body too many",
     "tool_coordinator.go: `active < limit` becomes `active <= limit`",
     "CanStart == next < N /\\ ~exclFlag /\\ active < Limit",
     "CanStart == next < N /\\ ~exclFlag /\\ active <= Limit",
     {"PoolBound"}),
    ("ToolCall entry not written before the body",
     "tool_coordinator.go: prepare no longer calls b.commit(sessions.ToolCall{...})",
     "\\/ /\\ recorded' = [recorded EXCEPT ![s] = TRUE]", "\\/ /\\ UNCHANGED recorded",
     {"RecordBeforeBody"}),
    ("a body starts after the abort",
     "tool_coordinator.go: execute's `b.ctx.Err() != nil ||` check in once.Do removed",
     "IF aborted \\/ cancelled", "IF cancelled",
     {"NoBodyAfterAbort"}),
    ("runToolBatch returns without draining started bodies",
     "tool_coordinator.go: the deferred `for active > 0 { <-done }` loop removed",
     "/\\ pc = \"drain\" /\\ active = 0", "/\\ pc = \"drain\"",
     {"DrainedOnReturn"}),
    ("repair answers a call that already has a result",
     "tool_repair.go: the `if results[key] > 0 { continue }` skip removed",
     "LAMBDA i : ~Answered(i)", "LAMBDA i : TRUE",
     {"AtMostOneResult", "ResultsInSourceOrder"}),
    ("repair says not started for a recorded call",
     "tool_repair.go: `if recorded[key] { text = \"outcome unknown\" }` removed",
     "IF recorded[Missing[k]] THEN \"unknown\" ELSE \"notStarted\"", "\"notStarted\"",
     {"TruthfulOutcome"}),
    ("a failed batch skips repair",
     "agent.go: `repairTools(r.source, repairStart)` no longer called after an error",
     "pc' = IF failed THEN \"repair\" ELSE \"done\"", "pc' = \"done\"",
     {"EveryCallAnswered"}),
]


def skill_record():
    text = open(os.path.join(SKILL_DIR, "SKILL.md")).read()
    sync = re.search(r"Sync commit: `([0-9a-f]{40})`", text)
    coverage = text.split("## Coverage", 1)[1].split("\n## ", 1)[0]
    files = sorted(set(re.findall(r"`((?:internal|cmd|pkg)/[\w/.-]+\.go)`", coverage)))
    if not sync or not files:
        sys.exit("SKILL.md lost its `Sync commit:` line or its Coverage file list")
    return sync.group(1), files


def tla_jar():
    jar = os.environ.get("TLA2TOOLS_JAR") or os.path.expanduser(
        f"~/.cache/tlaplus/tla2tools-{TLA_VERSION}.jar")
    if not os.path.exists(jar):
        os.makedirs(os.path.dirname(jar), exist_ok=True)
        print(f"downloading {TLA_URL}", file=sys.stderr)
        urllib.request.urlretrieve(TLA_URL, jar + ".part")
        os.replace(jar + ".part", jar)
    digest = hashlib.sha256(open(jar, "rb").read()).hexdigest()
    if digest != TLA_SHA256:
        sys.exit(f"{jar} is not tla2tools {TLA_VERSION} (sha256 {digest})")
    if not shutil.which("java"):
        sys.exit("java (11 or newer) is not on PATH")
    return jar


def run_tlc(jar, spec_text, cfg_text):
    with tempfile.TemporaryDirectory(prefix="toolbatch-") as tmp:
        open(os.path.join(tmp, "ToolBatch.tla"), "w").write(spec_text)
        open(os.path.join(tmp, "ToolBatch.cfg"), "w").write(cfg_text)
        out = subprocess.run(
            ["java", "-XX:+UseParallelGC", "-cp", jar, "tlc2.TLC", "-workers", "auto",
             "-metadir", os.path.join(tmp, "states"), "-config", "ToolBatch.cfg", "ToolBatch.tla"],
            cwd=tmp, capture_output=True, text=True).stdout
    if "Model checking completed. No error has been found." in out:
        return None, out
    m = re.search(r"(?:Invariant|Action property) (\w+) is violated", out)
    if m:
        return m.group(1), out
    if "Temporal properties were violated" in out:
        return "Terminates", out
    if "Deadlock reached" in out:
        return "Deadlock", out
    return "TLC error", out


def model():
    spec = open(os.path.join(MODEL_DIR, "ToolBatch.tla")).read()
    cfg = open(os.path.join(MODEL_DIR, "ToolBatch.cfg")).read()
    return spec, cfg


def check():
    sync, files = skill_record()
    jar = tla_jar()
    spec, cfg = model()
    failed, out = run_tlc(jar, spec, cfg)
    if failed:
        print(out)
        print(f"FAIL: {failed}. Read the trace above with /statechart before you touch the model.")
        return 1
    stats = re.findall(r"(\d[\d,]*) distinct states found", out)
    print(f"PASS: every property holds ({stats[-1] if stats else '?'} distinct states).")
    drift = subprocess.run(["git", "-C", REPO, "diff", "--stat", sync, "--", *files],
                           capture_output=True, text=True, check=True).stdout
    if drift.strip():
        print(f"STALE: covered code changed since the sync commit {sync[:12]}:")
        print(drift)
        print(f"Read `git diff {sync[:12]} -- <file>` and the Coverage table, mirror each "
              "change in the model, and rerun.")
        return 3
    print(f"IN SYNC: no covered file changed since {sync[:12]}.")
    return 0


def mutants():
    jar = tla_jar()
    spec, cfg = model()
    cfg = re.sub(r"N = \d+", "N = 3", cfg)
    bad = 0
    for name, code, old, new, expected in MUTANTS:
        if spec.count(old) != 1:
            print(f"BROKEN MUTANT {name!r}: the model text {old!r} is not there exactly once")
            bad += 1
            continue
        failed, _ = run_tlc(jar, spec.replace(old, new), cfg)
        ok = failed in expected
        bad += not ok
        print(f"{'caught' if ok else 'MISSED'}: {name} -> {failed or 'no violation'}  [{code}]")
    return 1 if bad else 0


if __name__ == "__main__":
    arg = sys.argv[1] if len(sys.argv) > 1 else "check"
    if arg not in ("check", "mutants"):
        sys.exit(__doc__)
    sys.exit(check() if arg == "check" else mutants())
