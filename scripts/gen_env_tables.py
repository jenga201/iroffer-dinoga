#!/usr/bin/env python3
"""Generate the env-var tables in entrypoint.sh, the ENV block in the
Dockerfile, and the directive section of .env.example from the iroffer-dinoex
source tree pinned by IROFFER_SHA256 in the Dockerfile.

Usage:
    ./scripts/gen_env_tables.py /path/to/iroffer-src [sample.config]

    /path/to/iroffer-src   extracted iroffer-dinoex source (needs src/dinoex_config.c)
    sample.config          optional; defaults to <src>/sample.config

What it does:
  * parses the config parse tables in src/dinoex_config.c
    (config_parse_bool/int/string/list/func), honoring #ifdef guards:
    USE_GEOIP / USE_MAXMINDDB directives are excluded (not compiled with
    the Dockerfile's './Configure -curl -upnp -ruby');
  * classifies every directive as single-value / multi-value (';;'),
    bool (yes|no), and global / network-scoped, by inspecting the c_<name>()
    handler bodies for network-bracket awareness and list-append semantics;
  * writes three artifacts under /tmp/gen/:
      tables_block.txt   - paste over the region between
                           '# ---- BEGIN GENERATED TABLES' and
                           '# ---- END GENERATED TABLES ----' in entrypoint.sh
      dockerfile_env.txt - ENV block for the Dockerfile
      sample_info.json   - per-directive docs/examples from sample.config
                           (used to refresh .env.example)

After changing the pinned snapshot (IROFFER_URL/IROFFER_SHA256), re-run this
script, copy the generated blocks into entrypoint.sh/Dockerfile/.env.example,
and commit.

Known limitation: #ifdef tracking ignores #else/#elif branches (entries in an
#else branch inherit the enclosing #if condition). Verified harmless for the
tables in the pinned snapshot.
"""
import json
import re
import sys
import os

MULTI_FUNCS = {
    # c_<name> handlers that append to a list per line
    "server", "channel", "proxyinfo", "periodicmsg", "mime_type",
    "autoadd_group_match", "group_admin", "server_join_raw",
    "server_connected_raw", "channel_join_raw", "connectionmethod",
}
# directives never exposed generically (managed via IROFFER_NETWORK_*)
EXCLUDE = {"network"}


def parse_table(src, marker):
    i = src.index(marker)
    b = src.index("{", i)
    depth = 0
    for j in range(b, len(src)):
        if src[j] == "{":
            depth += 1
        elif src[j] == "}":
            depth -= 1
            if depth == 0:
                break
    entries = []
    cond_stack = []
    for line in src[b:j + 1].splitlines():
        st = line.strip()
        if st.startswith("#if"):
            cond_stack.append(st)
        elif st.startswith("#endif"):
            if cond_stack:
                cond_stack.pop()
        m = re.match(r'^\{"([a-z_0-9{]+)"', st)
        if m:
            entries.append((m.group(1), list(cond_stack)))
    return entries


def supported(conds):
    for c in conds:
        if "USE_GEOIP" in c or "USE_MAXMINDDB" in c:
            return False  # not compiled in the docker image
    return True


def handler_body(src, key):
    pat = r"\nstatic [^\n]*\bc_%s\([^\n]*\)\s*\{" % re.escape(key)
    m = re.search(pat, src)
    if not m:
        return None
    rest = src[m.end():]
    end = rest.find("\n}\n")
    return rest[:end]


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(2)
    srcdir = sys.argv[1]
    cfg_c = open(os.path.join(srcdir, "src/dinoex_config.c")).read()
    sample_path = sys.argv[2] if len(sys.argv) > 2 else os.path.join(srcdir, "sample.config")

    tbls = {
        "bool": parse_table(cfg_c, "static config_bool_typ config_parse_bool[]"),
        "int": parse_table(cfg_c, "static config_int_typ config_parse_int[]"),
        "string": parse_table(cfg_c, "static config_string_typ config_parse_string[]"),
        "list": parse_table(cfg_c, "static config_list_typ config_parse_list[]"),
        "func": parse_table(cfg_c, "static config_func_typ config_parse_func[]"),
    }
    tbls = {k: list(dict.fromkeys(n for n, c in v if supported(c) and n not in ("{", "}")))
            for k, v in tbls.items()}

    directives = {}
    for n in tbls["bool"]:
        directives.setdefault(n, {"type": "bool"})
    for n in tbls["int"]:
        directives.setdefault(n, {"type": "int"})
    for n in tbls["string"]:
        directives.setdefault(n, {"type": "string"})
    for n in tbls["list"]:
        directives.setdefault(n, {"type": "list"})["multi"] = True
    for n in tbls["func"]:
        directives.setdefault(n, {"type": "func"})
        if n in MULTI_FUNCS:
            directives[n]["multi"] = True

    bool_set = set(tbls["bool"])
    block_aware = set()
    for n in tbls["func"]:
        body = handler_body(cfg_c, n)
        if body is None:
            continue
        if "parse_bool_to_ptr" in body or "parse_bool_val" in body:
            bool_set.add(n)
        if ("current_bracket" in body) or ("gdata.networks" in body) or ("network_index" in body):
            block_aware.add(n)
    for n in directives:
        directives[n]["scope"] = "network" if n in block_aware else "global"
        directives[n].setdefault("multi", False)

    single = sorted(k for k, d in directives.items() if not d["multi"] and k not in EXCLUDE)
    multi = sorted(k for k, d in directives.items() if d["multi"] and k not in EXCLUDE)
    bools = sorted(k for k in bool_set if k not in EXCLUDE)
    net = sorted(k for k, d in directives.items() if d["scope"] == "network" and k not in EXCLUDE)

    def bl(keys, per_line=6):
        return "\n".join("  " + " ".join(keys[i:i + per_line]) for i in range(0, len(keys), per_line))

    block = f"""# ---- BEGIN GENERATED TABLES (from iroffer-dinoex config parser, snapshot pinned in Dockerfile) ----
# Single-value directives: IROFFER_<NAME> replaces the active config line (overrides region, prepended).
SINGLE_DIRECTIVES=(
{bl(single)}
)

# Multi-value directives: IROFFER_<NAME> accepts ';;'-separated values; all active lines for the
# directive outside network blocks are removed and one line per value is added.
MULTI_DIRECTIVES=(
{bl(multi)}
)

# Bool-typed directives (validated: value must be yes|no).
BOOL_DIRECTIVES=(
{bl(bools)}
)

# Network-scoped directives usable inside IROFFER_NETWORK_<n>_* blocks.
NETWORK_DIRECTIVES=(
{bl(net)}
)
# ---- END GENERATED TABLES ----"""

    os.makedirs("/tmp/gen", exist_ok=True)
    with open("/tmp/gen/tables_block.txt", "w") as f:
        f.write(block + "\n")

    # Dockerfile ENV block
    envs = ["IROFFER_" + k.upper() for k in sorted(directives) if k not in EXCLUDE] + ["IROFFER_NETWORK_COUNT"]
    lines = ["    " + " ".join(e + '=""' for e in envs[i:i + 3]) for i in range(0, len(envs), 3)]
    with open("/tmp/gen/dockerfile_env.txt", "w") as f:
        f.write("ENV \\\n" + " \\\n".join(lines) + "\n")

    # sample.config docs for .env.example
    info = {}
    big = sub = None
    doc = []
    for line in open(sample_path).read().splitlines():
        s = line.strip()
        if re.match(r"^##\s+[A-Z0-9]", s) and not s.startswith("###"):
            big, sub, doc = s.strip("#").strip(), None, []
            continue
        if s.startswith("###"):
            t = s.strip("#").strip()
            m = re.match(r"^-\s*(.+?)\s*-$", t)
            if m:
                sub, doc = m.group(1), []
            else:
                doc.append(t)
            continue
        if s.startswith("####"):
            continue
        if not s:
            doc = []
            continue
        m = re.match(r"^#?\s*([a-z_0-9]+)\s*(.*)$", s)
        if m and m.group(1) in directives and m.group(1) not in info:
            info[m.group(1)] = {"section": big or "OTHER", "sub": sub or "misc",
                                "doc": re.sub(r"\s+", " ", " ".join(doc)).strip()[:150],
                                "example": m.group(2).strip()}
        doc = []
    with open("/tmp/gen/sample_info.json", "w") as f:
        json.dump(info, f, indent=1)

    print(f"directives: {len(directives)} (single {len(single)}, multi {len(multi)}, "
          f"bool {len(bools)}, network {len(net)})")
    print("wrote /tmp/gen/tables_block.txt, /tmp/gen/dockerfile_env.txt, /tmp/gen/sample_info.json")
    print("Paste tables_block.txt over the GENERATED TABLES region in entrypoint.sh")
    print("and dockerfile_env.txt into the Dockerfile ENV section.")


if __name__ == "__main__":
    main()
