#!/usr/bin/env python3
"""Check that an update manifest describes the asset it points at.

Run by .github/workflows/updates.yml whenever a manifest changes, and worth
running by hand before pushing one:

    python3 .github/verify-manifest.py updates/release.json

Every check here is one that a person cutting a release by hand can get wrong,
and that nothing else would notice until somebody's app tried to install it.
"""
import hashlib
import json
import os
import re
import sys
import urllib.request

REPO = "lijuno/agent-pet"
SEMVER = re.compile(r"^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$")
FIELDS = {"channel", "version", "url", "sha256", "size",
          "min_macos", "published", "notes_url"}


def fail(msg):
    print(f"  FAIL {msg}")
    sys.exit(1)


def main(path):
    channel = os.path.basename(path)[: -len(".json")]
    if channel not in ("release", "dev"):
        fail(f"{path} is not a channel manifest (expected release.json or dev.json)")

    try:
        with open(path) as f:
            m = json.load(f)
    except FileNotFoundError:
        fail(f"{path} does not exist")
    except json.JSONDecodeError as e:
        fail(f"{path} is not valid JSON: {e}")

    extra = set(m) - FIELDS
    if extra:
        # The app refuses unknown fields outright, so one here would make every
        # installed pet reject the manifest — a silent no-op that looks exactly
        # like a release nobody noticed.
        fail(f"unknown field(s) {sorted(extra)}; the app would refuse this file")

    if m.get("channel") != channel:
        fail(f"{path} says channel={m.get('channel')!r}")
    if not SEMVER.match(m.get("version", "")):
        fail(f"version {m.get('version')!r} is not a version number")
    if channel == "release" and "-" in m["version"]:
        fail(f"{m['version']} is a prerelease and must not be on the release channel")

    url = m.get("url", "")
    want = f"https://github.com/{REPO}/releases/download/v{m['version']}/"
    if not url.startswith(want):
        fail(f"url is not a v{m['version']} release asset of {REPO}:\n       {url}")
    if not re.fullmatch(r"[0-9a-f]{64}", m.get("sha256", "")):
        fail("sha256 is not 64 lowercase hex characters")

    print(f"  {channel}: offering {m['version']}")
    print(f"  fetching {url}")
    try:
        with urllib.request.urlopen(url, timeout=120) as r:
            body = r.read()
    except Exception as e:                                  # noqa: BLE001
        fail(f"the asset could not be downloaded: {e}\n"
             f"       The manifest is published but the file is not — every app that "
             f"checks will fail.")

    if len(body) != m.get("size"):
        fail(f"size says {m.get('size')} and the asset is {len(body)} bytes")
    got = hashlib.sha256(body).hexdigest()
    if got != m["sha256"]:
        fail(f"sha256 mismatch\n       manifest {m['sha256']}\n       asset    {got}")

    print(f"  {len(body)} bytes, sha256 matches")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: verify-manifest.py updates/<channel>.json")
    main(sys.argv[1])
