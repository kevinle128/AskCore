#!/usr/bin/env python3
"""Rebuild design.md from design-template.md and sketch-gen.py, then check widths.

Run from the plan directory: python3 artifacts/build-design.py
"""
import pathlib
import re
import subprocess
import sys

here = pathlib.Path(__file__).resolve().parent
plan = here.parent
sketches = subprocess.run([sys.executable, str(here / "sketch-gen.py")],
                          check=True, capture_output=True, text=True).stdout
body = (here / "design-template.md").read_text()

# Number the acceptance criteria in order, so new items can be added as "3a.".
head, sep, tail = body.partition("## 10. Acceptance criteria\n")
section, sep2, rest = tail.partition("## 11.")
count = 0


def renumber(_):
    global count
    count += 1
    return f"{count}."


section = re.sub(r"^\d+[a-z]?\.", renumber, section, flags=re.M)
body = head + sep + section + sep2 + rest
out = body.replace("{{SKETCHES}}", sketches.replace("### ", "#### ").strip())
(plan / "design.md").write_text(out)
subprocess.run([sys.executable, str(here / "verify-widths.py"), str(plan / "design.md")], check=True)
subprocess.run([sys.executable, str(here / "preview-gen.py")], check=True)
