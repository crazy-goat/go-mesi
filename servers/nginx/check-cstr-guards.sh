#!/bin/bash
# Static regression guard for #429: every ngx_str_to_cstr() result must be
# NULL-checked before it is used. ngx_str_to_cstr() returns NULL when the
# request/pool allocation fails; an unchecked result is a worker crash
# (strcmp/ngx_strlen on NULL) or a NULL crossing the cgo ABI.
#
# There is no C unit harness for the nginx module and forcing ngx_palloc to
# fail at runtime is not deterministic, so the guard is enforced statically:
# for every `char *name = ... ngx_str_to_cstr(...);` declaration the same
# enclosing function must compare `name` against NULL. This is the invariant
# the fix relies on, and it catches a new unchecked call site.
set -euo pipefail
cd "$(dirname "$0")"

python3 - <<'PY'
import pathlib
import re
import sys

source = pathlib.Path("ngx_http_mesi_module.c").read_text()

decl = re.compile(
    r"\bchar\s*\*\s*(?P<name>[A-Za-z_]\w*)\s*=(?P<rhs>.*?);",
    re.DOTALL,
)

issues = []
checked = 0

for match in decl.finditer(source):
    if "ngx_str_to_cstr" not in match.group("rhs"):
        continue

    name = match.group("name")
    line = source.count("\n", 0, match.start()) + 1

    # Enclosing function body: everything up to the next column-0 closing
    # brace (all functions in this file close at column 0).
    end = source.find("\n}", match.end())
    if end == -1:
        end = len(source)
    body = source[match.end():end]

    null_checks = (
        rf"\b{re.escape(name)}\s*==\s*NULL\b",
        rf"\bNULL\s*==\s*{re.escape(name)}\b",
        rf"!\s*{re.escape(name)}\b",
    )
    if any(re.search(pattern, body) for pattern in null_checks):
        checked += 1
    else:
        issues.append((line, name))

if issues:
    for line, name in issues:
        print(
            f"FAIL: ngx_str_to_cstr() result '{name}' (line {line}) is "
            f"never NULL-checked"
        )
    sys.exit(1)

if checked == 0:
    print("FAIL: found no ngx_str_to_cstr() declarations; has the file moved?")
    sys.exit(1)

print(f"PASS: all {checked} ngx_str_to_cstr() results are NULL-checked")
PY
