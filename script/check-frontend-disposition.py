#!/usr/bin/env python3
"""Gate actual embedded frontend findings without hiding raw npm results.

Only exact reviewed package/version/advisory triples can be classified.
Any unrecognized finding, malformed report, registry error or missing
mitigation evidence fails the job. Browser regression is a separate
mandatory release check.
"""
import json
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[1]
vendor = root / "resource/static/vendor"
reports = json.loads((root / "frontend-audit.json").read_text())
approved = {
    ("bootstrap", "3.4.1", "GHSA-vxmc-5x29-h64v"): "not shipped: button plugin",
    ("bootstrap", "3.4.1", "GHSA-q58r-hwc8-rm9j"): "not shipped: tooltip and popover plugins",
    ("vue", "2.7.16", "GHSA-5j4c-8p2g-v4jx"): "locally patched plain-text parser; browser regression required",
}
bootstrap = vendor / "bootstrap-3.4.1/dist/js/bootstrap-server-status.js"
vue = vendor / "vue-2.7.16/dist/vue.min.js"
bootstrap_js = bootstrap.read_text()
vue_js = vue.read_text()
assert not (bootstrap.parent / "bootstrap.min.js").exists(), "full Bootstrap JS unexpectedly shipped"
for plugin in ("button", "tooltip", "popover"):
    assert "bs." + plugin not in bootstrap_js, "excluded plugin present: " + plugin
assert "Nezha local CVE-2024-9506 compiler patch" in vue_js
assert "[Symbol.replace](html,callback)" in vue_js
assert len(list((vendor / "vue-2.7.16").rglob("*.js"))) == 1, "unexpected Vue distributions"
dispositions = []
blocked = []
if not reports:
    blocked.append("empty audit report")
for report in reports:
    audit = report.get("audit")
    if not isinstance(audit, dict) or "error" in audit or "error" in report:
        blocked.append("npm audit or dependency resolution error")
        continue
    findings = audit.get("vulnerabilities")
    if not isinstance(findings, dict) or (report.get("exitCode") and not findings):
        blocked.append("invalid npm audit result")
        continue
    for name, finding in findings.items():
        version = report["dependencies"].get(name)
        via = finding.get("via", [])
        if not via:
            blocked.append("missing advisory for " + name)
        for advisory in via:
            if not isinstance(advisory, dict):
                blocked.append("unreviewed transitive finding: " + name + " via " + str(advisory))
                continue
            url = advisory.get("url", "")
            key = (name, version, url.rsplit("/",1)[-1])
            reason = approved.get(key) if url.startswith("https://github.com/advisories/") else None
            entry = {"package":name,"version":version,"advisory":url,"disposition":reason or "BLOCKED"}
            dispositions.append(entry)
            if not reason:
                blocked.append(entry)
summary = {"reviewed_findings":dispositions,"blocked":blocked,
           "note":"Original npm audit findings remain in frontend-audit.json. Local mitigations require browser regression; EOL frameworks remain a maintenance risk."}
(root / "frontend-disposition.json").write_text(json.dumps(summary,indent=2)+"\n")
print(json.dumps(summary,indent=2))
sys.exit(1 if blocked else 0)
