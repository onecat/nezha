#!/usr/bin/env python3
"""Localize bundled templates using exact npm package versions at build time."""
import pathlib
import re
import subprocess
import tempfile
import tarfile
import shutil

ROOT = pathlib.Path(__file__).resolve().parents[1]
RESOURCE = ROOT / "resource"
VENDOR = RESOURCE / "static" / "vendor"
VENDOR.mkdir(parents=True, exist_ok=True)
ALIASES = {
    "font-awesome": "@fortawesome/fontawesome-free",
    "limonte-sweetalert2": "sweetalert2",
}
PINS = {"mdui@2": "2.1.3", "font-logos@0.17": "0.17.0"}
PACKAGES = {}
TEMP = tempfile.TemporaryDirectory()
WORK = pathlib.Path(TEMP.name)

def get_package(name, version):
    name = ALIASES.get(name, name)
    version = PINS.get(name + "@" + version, version)
    key = (name, version)
    if key not in PACKAGES:
        folder = WORK / str(len(PACKAGES))
        folder.mkdir()
        print("Fetching", name, version, flush=True)
        result = subprocess.run(
            ["npm", "pack", "--ignore-scripts", "--silent", name + "@" + version],
            cwd=folder, check=True, capture_output=True, text=True,
        )
        archive = folder / result.stdout.strip().splitlines()[-1]
        with tarfile.open(archive) as tar:
            tar.extractall(folder, filter="data")
        base = folder / "package"
        dest = VENDOR / (name.replace("/", "-").lstrip("@") + "-" + version)
        PACKAGES[key] = (base, dest)
        for license_file in base.iterdir():
            if license_file.is_file() and license_file.name.lower().startswith(("license", "licence", "notice", "copying")):
                dest.mkdir(parents=True, exist_ok=True)
                shutil.copy2(license_file, dest / license_file.name)
    return PACKAGES[key]

def copy_asset(base, dest, relative):
    relative = relative.lstrip("/")
    source = base / relative
    if not source.exists():
        candidates = [
            relative.replace("font-logos.min.css", "font-logos.css"),
            "dist/" + relative,
            relative.replace("css/", "dist/css/", 1),
            relative.replace("js/", "dist/js/", 1),
            relative.replace("sweetalert2.all.min.js", "dist/sweetalert2.all.min.js"),
        ]
        for candidate in candidates:
            if (base / candidate).exists():
                source = base / candidate
                break
    if not source.exists():
        raise RuntimeError(f"Asset not found: {base} / {relative}")
    target = dest / relative
    if source.is_dir():
        shutil.copytree(source, target, dirs_exist_ok=True)
        return
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.exists():
        return
    shutil.copy2(source, target)
    if source.suffix == ".css":
        css = source.read_text(encoding="utf-8")
        for ref in re.findall(r"""url\(\s*['"]?([^'")]+)""", css):
            ref = ref.strip().split("?")[0].split("#")[0]
            if not ref or ref.startswith(("data:", "http:", "https:", "//", "/")):
                continue
            dep = (source.parent / ref).resolve()
            if dep.is_file() and dep.is_relative_to(base.resolve()):
                target_dep = (target.parent / ref).resolve()
                if not target_dep.is_relative_to(dest.resolve()):
                    raise RuntimeError("CSS dependency escapes package")
                target_dep.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(dep, target_dep)

def localize(match):
    url = match.group(0)
    if "unpkg.com/" in url:
        value = url.split("unpkg.com/", 1)[1]
        m = re.fullmatch(r"((?:@[^/]+/)?[^/@]+)@([^/]+)/(.+)", value)
        if not m:
            raise RuntimeError("Unsupported npm CDN path: " + url)
        name, version, relative = m.groups()
    else:
        value = re.sub(r"^https://[^/]+/cdn/expire-[^/]+/", "", url)
        name, version, relative = value.split("/", 2)
    # Normalize CDN aliases to their actual npm paths before copying.
    if name == "bootstrap-icons" and version == "1.8.1":
        version = "1.11.3"
    if name == "font-logos" and relative == "font-logos.min.css":
        relative = "assets/font-logos.css"
    if name == "limonte-sweetalert2":
        relative = "dist/" + relative if not relative.startswith("dist/") else relative
    base, dest = get_package(name, version)
    copy_asset(base, dest, relative)
    return "/static/vendor/" + dest.name + "/" + relative

CDN = re.compile(r"""https://(?:unpkg\.com/|lf[0-9]+-cdn-tos\.bytecdntp\.com/cdn/expire-[^/]+/)[^'"\s<>{}()]+""")
for file in sorted((RESOURCE / "template").rglob("*.html")):
    s = file.read_text(encoding="utf-8")
    s = re.sub(r"""<link\b[^>]*https://fonts\.googleapis\.com/[^>]*>""", "", s)
    s = CDN.sub(localize, s)
    s = re.sub(r"""<a\b([^>]*?)href=["']https://(?:github\.com/(?:naiba|nezhahq)/nezha|blog\.jackiesung\.com|yujie\.pro)/?["']([^>]*)>(.*?)</a>""", r"<span\1\2>\3</span>", s, flags=re.S)
    for match in re.finditer(r"""(?:src|href)\s*=\s*["']https?://[^"']+""", s, flags=re.I):
        if not match.group(0).lower().startswith("href="):
            raise RuntimeError(f"External asset in {file}: {match.group(0)}")
    if re.search(r"https://(?:unpkg\.com|[^/]+\.bytecdntp\.com|fonts\.googleapis\.com|raw\.githubusercontent\.com/nezhahq/scripts)", s):
        raise RuntimeError(f"External runtime dependency remains in {file}")
    file.write_text(s, encoding="utf-8")

print("Embedded frontend packages:", len(PACKAGES))
