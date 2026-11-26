#!/usr/bin/env python3
"""Audit the npm versions actually selected by vendor-assets.py.

Multiple versions of the same library occur in different themes. Put them in
separate lockfiles rather than silently overwriting older versions. npm may
also report transitive dependencies that are not actually embedded.
"""
import json
import pathlib
import subprocess
import tempfile

root = pathlib.Path(__file__).resolve().parents[1]
inventory = json.loads((root / ".frontend-packages.json").read_text())
groups = []
for item in inventory:
    for group in groups:
        if item["name"] not in group:
            group[item["name"]] = item["version"]
            break
    else:
        groups.append({item["name"]: item["version"]})
reports = []
failed = False
with tempfile.TemporaryDirectory() as tmp:
    for i, dependencies in enumerate(groups):
        work = pathlib.Path(tmp) / str(i)
        work.mkdir()
        (work / "package.json").write_text(json.dumps({
            "name": "nezha-frontend-audit", "version": "1.0.0",
            "private": True, "dependencies": dependencies
        }))
        install = subprocess.run(
            ["npm", "install", "--package-lock-only", "--ignore-scripts",
             "--no-audit", "--no-fund", "--omit=dev", "--legacy-peer-deps"],
            cwd=work, capture_output=True, text=True,
        )
        if install.returncode:
            reports.append({"dependencies": dependencies, "error": install.stderr})
            failed = True
            continue
        audit = subprocess.run(["npm", "audit", "--omit=dev", "--json"],
                               cwd=work, capture_output=True, text=True)
        try:
            result = json.loads(audit.stdout)
        except ValueError:
            result = {"error": audit.stderr or audit.stdout}
        reports.append({"dependencies": dependencies, "audit": result,
                        "exitCode": audit.returncode})
        failed |= bool(audit.returncode or "error" in result)
        print("Audit group", i, "exit", audit.returncode, flush=True)
        for name, finding in sorted(result.get("vulnerabilities", {}).items()):
            if name in dependencies:
                print("Direct finding", name, dependencies[name], finding.get("severity"), flush=True)
                for via in finding.get("via", []):
                    if isinstance(via, dict):
                        print("  ", via.get("url"), via.get("title"), flush=True)
        if "error" in result:
            print("Audit error:", result["error"], flush=True)
(root / "frontend-audit.json").write_text(json.dumps(reports, indent=2) + "\n")
raise SystemExit(1 if failed else 0)
