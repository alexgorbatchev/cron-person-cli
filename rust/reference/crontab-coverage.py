"""Replay actual reference calls in a covered Go executable, including child processes."""
import json
import os
from pathlib import Path
import subprocess
import sys

probe = str(Path(sys.argv[1]).resolve())
coverage = str(Path(sys.argv[2]).resolve())
cases = [json.loads(line) for line in Path("rust/crontab-rs/cases.jsonl").read_text().splitlines()]
environment = dict(os.environ, GOCOVERDIR=coverage)
result = subprocess.run([probe], input="".join(json.dumps({"fn": c["fn"], "args": c["args"]}) + "\n" for c in cases), text=True, capture_output=True, env=environment, check=True)
actual = [json.loads(line) for line in result.stdout.splitlines()]
assert len(actual) == len(cases)
for i, (got, case) in enumerate(zip(actual, cases)):
    expected = {k: case[k] for k in ("ok", "error") if k in case}
    assert got == expected, (i, got, expected)

# Actual fixture creation failure, with only the subprocess's environment changed.
call = {"fn": "Process", "args": [json.dumps({"operation": "read", "mode": "normal"})]}
failed = subprocess.run([probe], input=json.dumps(call) + "\n", text=True, capture_output=True, env=dict(environment, TMPDIR=coverage + "/absent"), check=True)
assert "error" in json.loads(failed.stdout)
# Real input/output failures in the same production-call child protocol.
directory = os.open(".", os.O_RDONLY)
try:
    child = subprocess.run([probe], stdin=directory, capture_output=True, env=dict(environment, CRONTAB_REFERENCE_CHILD="read"))
    assert child.returncode == 2
finally:
    os.close(directory)
with open("/dev/full", "wb") as full:
    child = subprocess.run([probe], input=b"", stdout=full, stderr=subprocess.PIPE, env=dict(environment, PATH=coverage + "/absent", CRONTAB_REFERENCE_CHILD="read"))
    assert child.returncode == 2
    # Probe serialization errors must also remain real errors.
    child = subprocess.run([probe], input=json.dumps(cases[0]).encode(), stdout=full, stderr=subprocess.PIPE, env=environment)
    assert child.returncode == 1
for invalid in [b"{", b'{"fn":"unknown","args":[]}']:
    child = subprocess.run([probe], input=invalid, capture_output=True, env=environment)
    assert child.returncode != 0
subprocess.run(["go", "test", "-count=1", "-covermode=atomic", "-cover", "./rust/reference/crontab"], env=dict(os.environ, CRONTAB_COUNTERS=coverage), check=True)
profile = str(Path(coverage).parent / "reference.cover")
subprocess.run(["go", "tool", "covdata", "textfmt", "-i=" + coverage, "-o=" + profile], check=True)
totals = {}
for line in Path(profile).read_text().splitlines()[1:]:
    span, statements, count = line.split()
    source = span.split(":")[0]
    total, covered = totals.get(source, (0, 0))
    total += int(statements)
    covered += int(statements) if int(count) else 0
    totals[source] = total, covered
for source, (total, covered) in totals.items():
    percent = covered * 100 / total
    assert percent >= 90, (source, covered, total, percent)
    print(f"{source}: {covered}/{total} statements ({percent:.2f}%)")
print(f"reference exact replay {len(cases)}/{len(cases)}; fixture/input/output failures verified")
