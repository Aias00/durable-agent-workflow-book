#!/usr/bin/env python3
"""Turn raw go test -json into evidence; fail publication if integration skipped."""
import json
import os
import pathlib
import subprocess
import sys
from datetime import datetime, timezone

folder = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "evidence")
tests = {}
observations = []
package_failures = []
for line in (folder / "tests.jsonl").read_text().splitlines():
    item = json.loads(line)
    name = item.get("Test")
    if name and item["Action"] in ("pass", "fail", "skip"):
        tests[name] = item["Action"]
    if item["Action"] == "fail" and not name:
        package_failures.append(item["Package"])
    output = item.get("Output", "")
    marker = "BOOK_OBSERVATION "
    if marker in output:
        observations.append({"test": name, "observation": json.loads(output.split(marker, 1)[1])})

passed = bool(tests) and len(tests) >= 25 and all(v == "pass" for v in tests.values()) and not package_failures and bool(os.environ.get("BOOK_LAB_DSN"))
result = {
    "schema_version": 1,
    "status": "PASS" if passed else "FAIL",
    "timestamp_utc": datetime.now(timezone.utc).isoformat(),
    "commit": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
    "run_url": os.environ.get("BOOK_RUN_URL"),
    "go": subprocess.check_output(["go", "version"], text=True).strip(),
    "postgresql": (folder / "postgresql-version.txt").read_text().strip(),
    "postgres_image_digest": (folder / "postgres-image-digest.txt").read_text().strip(),
    "command": "go test -race -count=1 -json ./...",
    "integration_dsn_configured": bool(os.environ.get("BOOK_LAB_DSN")),
    "tests": tests,
    "package_failures": package_failures,
    "observations": observations,
    "limits": [
        "One side-effect node; not a general workflow engine or exactly-once system.",
        "Workflow and tool ledger share one PostgreSQL instance but use separate transactions.",
        "SIGKILL experiments wait for the actual three-second database lease; other boundary tests label SQL-injected expiry.",
        "Loopback development credentials, authoritative lab lookup, no multi-cluster availability or provider-authentication claim.",
    ],
}
(folder / "runtime.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
print(json.dumps({"status": result["status"], "tests": len(tests), "observations": len(observations), "skips": sum(v == "skip" for v in tests.values())}))
sys.exit(0 if passed else 1)
