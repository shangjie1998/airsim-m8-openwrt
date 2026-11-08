#!/usr/bin/env python3
"""M8 bridge for the MU300 SMS pool.

The AirSIM agent expects a vendor pool directory (/etc/mu300/sms-pool/msg) with
one file per message:

    dir: mt
    id: <id>
    from: <number>
    length: <N>          <- BYTE length of the body, not character length
    received: <epoch>
    scts: <scts>
    <blank line>
    <N bytes of body>\\n

F50 got that from a native SMS daemon. This device has its own native SMS stack
as well (e5-sms-receive), which keeps incoming messages in
/etc/e5-sms/inbox.json and is the only component that actually sees SIM
messages; ModemManager never receives the inbound URCs. So we mirror that file
instead. Outbound "sent" records are skipped: only direction "in" is inbox.
"""
import json
import os
import re
import sys
import time

POOL = "/etc/mu300/sms-pool/msg"
STATE = "/opt/airsim-m8/data/sms-pool-state.json"
INBOX = "/etc/e5-sms/inbox.json"
ID_RE = re.compile(r"^[0-9]{1,12}$")


def native_inbox():
    try:
        with open(INBOX, "r", encoding="utf-8") as f:
            data = json.load(f)
    except Exception:
        return []
    items = []
    for m in data.get("messages", []):
        if m.get("direction") != "in":
            continue
        mid = str(m.get("id", ""))
        text = m.get("text") or ""
        number = m.get("number") or ""
        epoch = int(m.get("epoch") or m.get("first_seen") or 0)
        if not ID_RE.match(mid) or not number or not text or epoch <= 0:
            continue
        items.append((mid, number, text, epoch, m.get("time") or "-"))
    return items


def write_entry(mid, number, text, epoch, scts):
    os.makedirs(POOL, exist_ok=True)
    body = text.rstrip("\n")
    payload = ("dir: mt\nid: %s\nfrom: %s\nlength: %d\nreceived: %d\nscts: %s\n\n%s\n" %
               (mid, number, len(body.encode("utf-8")), epoch, scts, body))
    tmp = os.path.join(POOL, ".%s.tmp" % mid)
    with open(tmp, "w", encoding="utf-8") as f:
        f.write(payload)
    os.chmod(tmp, 0o600)
    os.replace(tmp, os.path.join(POOL, mid))


def is_outgoing(path):
    """Outbound records start with "dir: mo" and never come from the inbox."""
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            head = f.read(64)
    except OSError:
        return False
    return head.startswith("dir: mo")


def load_state():
    try:
        with open(STATE) as f:
            return set(json.load(f))
    except Exception:
        return set()


def save_state(state):
    try:
        tmp = STATE + ".tmp"
        with open(tmp, "w") as f:
            json.dump(sorted(state), f)
        os.replace(tmp, STATE)
    except OSError:
        pass


def main():
    once = "--once" in sys.argv
    state = load_state()
    while True:
        items = native_inbox()
        ids = {it[0] for it in items}
        for mid, number, text, epoch, scts in items:
            if mid in state:
                # Already written once. If the pool file is gone the agent (or the
                # user) deleted it on purpose, so do not resurrect it.
                continue
            write_entry(mid, number, text, epoch, scts)
            state.add(mid)
        # Pool entries whose native message no longer exists are stale — except
        # outbound ("dir: mo") records that the SMS shim writes after a send;
        # those have no native inbox counterpart by design.
        if os.path.isdir(POOL):
            for name in os.listdir(POOL):
                if not ID_RE.match(name) or name in ids:
                    continue
                path = os.path.join(POOL, name)
                if is_outgoing(path):
                    continue
                try:
                    os.unlink(path)
                    state.discard(name)
                except OSError:
                    pass
        save_state(state)
        if once:
            return
        time.sleep(8)


if __name__ == "__main__":
    main()
