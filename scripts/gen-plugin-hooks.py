#!/usr/bin/env python3
"""Generate plugin/hooks/hooks.json from the Claude adapter's hook list.

The adapter's `Hooks` slice decides which Claude Code hooks the pet
understands. The plugin has to agree with it exactly: a hook listed here that
the adapter cannot translate wastes a subprocess on every occurrence, and one
missing means the pet silently stops reacting to something. Generating the
file removes the chance of them drifting.
"""
import collections
import json
import pathlib
import re
import sys

root = pathlib.Path(__file__).resolve().parent.parent
src = (root / "adapters/claude/translate.go").read_text()

m = re.search(r"var Hooks = \[\]string\{(.*?)\}", src, re.S)
if not m:
    sys.exit("could not find `var Hooks` in adapters/claude/translate.go")
names = re.findall(r'"([^"]+)"', m.group(1))
if not names:
    sys.exit("`var Hooks` parsed as empty")

# Matches adapters/claude.HookTimeout. The hook gives up on its own well inside
# a second; this is the backstop that stops a wedged process holding up the
# agent.
TIMEOUT = 5

hooks = collections.OrderedDict()
for name in names:
    # No matcher: these fire for every tool and every notification type. A
    # matcher would be a filter the pet has no reason to apply.
    hooks[name] = [{
        "hooks": [{
            "type": "command",
            "command": '"${CLAUDE_PLUGIN_ROOT}"/bin/petctl hook claude',
            "timeout": TIMEOUT,
        }]
    }]

out = root / "plugin/hooks/hooks.json"
out.write_text(json.dumps({"hooks": hooks}, indent=2) + "\n")
print(f"wrote {out.relative_to(root)} with {len(names)} hooks")
