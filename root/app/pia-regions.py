#!/usr/bin/python3
"""PIA region selection with persisted connection history.

State is a JSON file (default /config/pia-regions-state.json) of the form:

    {
      "version": 1,
      "current": "kenya",   # region chosen on the most recent boot
      "exhausted": false,   # true when there are no fresh candidates left
      "regions": {
        "kenya": {"status": "good", "last_tried": 1731012345, "last_good": 1731012345},
        "ca_toronto": {"status": "bad", "reason": "irc-banned", "last_tried": 1731012300}
      }
    }

Subcommands:
    pick                     choose the next region, print its id to stdout
    mark-good <id>           record a successful region
    mark-bad <id> <reason>   record a failed region
"""
import json
import os
import sys
import time

APP_DIR = os.environ.get("APP_DIR") or "/app"
STATE_PATH = os.environ.get("DINOGA_REGION_STATE") or "/config/pia-regions-state.json"
COOLDOWN = int(os.environ.get("VPN_PIA_REGION_COOLDOWN") or "21600")


def _load_regions():
    paths = [
        os.path.join(APP_DIR, "pia-servers.json"),
        os.path.join(os.environ.get("CONFIG_DIR") or "/config", "wireguard", "pia-regions.json"),
    ]
    for path in paths:
        try:
            with open(path, encoding="utf-8") as fh:
                raw = fh.read()
        except OSError:
            continue
        try:
            for line in raw.splitlines():
                line = line.strip()
                if line.startswith("{"):
                    data = json.loads(line)
                    return [(r["id"], bool(r.get("port_forward"))) for r in data.get("regions", []) if r.get("id")]
        except (ValueError, KeyError):
            continue
    return []


def _load_state():
    try:
        with open(STATE_PATH, encoding="utf-8") as fh:
            state = json.load(fh)
            if isinstance(state, dict) and isinstance(state.get("regions"), dict):
                return state
    except (OSError, ValueError):
        pass
    return {"version": 1, "regions": {}, "current": None, "exhausted": False}


def _save_state(state):
    state.setdefault("version", 1)
    state.setdefault("regions", {})
    os.makedirs(os.path.dirname(STATE_PATH) or ".", exist_ok=True)
    tmp = STATE_PATH + ".tmp"
    with open(tmp, "w", encoding="utf-8") as fh:
        json.dump(state, fh, indent=2, sort_keys=True)
    os.replace(tmp, STATE_PATH)


def _filtered(regions):
    want_pf = os.environ.get("VPN_AUTO_PORT_FORWARD", "").strip().lower() == "true"
    if not want_pf or not regions:
        return regions
    pf = [r for r in regions if r[1]]
    return pf or regions  # fall back to all regions when none advertise port forward


def _rank(rid, st, preferred, now):
    lt = st.get("last_tried", 0) if st else 0
    lg = st.get("last_good", 0) if st else 0
    status = st.get("status") if st else None

    if rid == preferred:
        if status == "bad":
            return (4, "preferred-bad", lt)
        if lt > lg:
            return (5, "preferred-unconfirmed", lt)
        return (0, "preferred", 0)

    if lt == 0:
        return (2, "untried", 0)
    if status == "good" and lg >= lt:
        return (1, "confirmed-good", -lg)
    if status == "bad":
        if now - lt >= COOLDOWN:
            return (3, "stale-bad", -lt)
        return (6, "recent-bad", lt)
    # Attempted but never confirmed (crashed before the recorder concluded).
    return (5, "unconfirmed", lt)


def cmd_pick():
    regions = _filtered(_load_regions())
    state = _load_state()
    preferred = os.environ.get("VPN_PIA_PREFERRED_REGION", "").strip()
    now = time.time()

    if not regions:
        sys.stdout.write("")
        return

    ranked = sorted(regions, key=lambda r: _rank(r[0], state["regions"].get(r[0]), preferred, now))
    chosen, _ = ranked[0]
    rank = _rank(chosen, state["regions"].get(chosen), preferred, now)[1]

    reg = state["regions"].setdefault(chosen, {})
    reg["last_tried"] = now
    state["current"] = chosen
    state["exhausted"] = rank not in ("preferred", "confirmed-good", "untried")
    _save_state(state)

    sys.stdout.write(chosen)


def cmd_mark_good(region):
    state = _load_state()
    now = time.time()
    reg = state["regions"].setdefault(region, {})
    reg.update({"status": "good", "last_good": now, "last_tried": now})
    reg.pop("reason", None)
    state["current"] = region
    state["exhausted"] = False
    _save_state(state)


def cmd_mark_bad(region, reason):
    state = _load_state()
    reg = state["regions"].setdefault(region, {})
    reg.update({"status": "bad", "last_tried": time.time(), "reason": reason})
    state["current"] = region
    _save_state(state)


def main(argv):
    if not argv:
        return cmd_pick()
    if argv[0] == "pick":
        return cmd_pick()
    if argv[0] == "mark-good" and len(argv) >= 2:
        return cmd_mark_good(argv[1])
    if argv[0] == "mark-bad" and len(argv) >= 3:
        return cmd_mark_bad(argv[1], " ".join(argv[2:]))
    sys.exit("usage: pia-regions.py pick | mark-good <id> | mark-bad <id> <reason>")


if __name__ == "__main__":
    main(sys.argv[1:])