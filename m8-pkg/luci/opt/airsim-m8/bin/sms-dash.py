#!/usr/bin/env python3
"""/usr/libexec/rpcd/mu300dash backend for the stock AirSIM LuCI SMS panel.

The stock panel expects a `mu300dash` ubus object with sms_list / sms_show /
sms_send / sms_job. On the VIPME-M8 there is no vendor dash daemon, but the
Agent keeps the native pool at /etc/mu300/sms-pool/msg and sends through
/api/sms/send, so this helper serves the same contract on top of those.

Arguments: <method>, JSON args on stdin, one JSON object on stdout.
"""
import json
import os
import sys
import time
import urllib.request

POOL = "/etc/mu300/sms-pool/msg"
STATE = "/opt/airsim-m8/data/luci-sms.json"
AGENT = "http://127.0.0.1:8575"
PAGE_SIZE = 10


def load_state():
    try:
        with open(STATE, "r", encoding="utf-8") as f:
            d = json.load(f)
            d.setdefault("map", {})
            d.setdefault("read", [])
            d.setdefault("next", 100)
            return d
    except Exception:
        return {"map": {}, "read": [], "next": 100}


def save_state(st):
    try:
        os.makedirs(os.path.dirname(STATE), exist_ok=True)
        tmp = STATE + ".new"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(st, f)
        os.replace(tmp, STATE)
    except Exception:
        pass


def parse_entry(path):
    """Native pool entry: key: value header lines, blank line, body."""
    with open(path, "r", encoding="utf-8", errors="replace") as f:
        text = f.read()
    head, sep, body = text.partition("\n\n")
    if not sep:
        return None
    fields = {}
    for line in head.splitlines():
        if ":" in line:
            k, v = line.split(":", 1)
            fields[k.strip()] = v.strip()
    if not fields.get("id"):
        return None
    try:
        ts = int(fields.get("received", "0"))
    except ValueError:
        ts = 0
    return {"pool_id": fields["id"], "from": fields.get("from", ""), "to": fields.get("to", ""),
            "dir": fields.get("dir", "mt"),
            "received": fields.get("received", "0"), "scts": fields.get("scts", ""), "body": body.rstrip("\n"),
            "ts": ts}


def collect():
    items = []
    try:
        names = [n for n in os.listdir(POOL) if n.isdigit()]
    except FileNotFoundError:
        return items
    for n in names:
        e = parse_entry(os.path.join(POOL, n))
        if e:
            items.append(e)
    items.sort(key=lambda x: x["ts"], reverse=True)
    return items


def short_id(st, pool_id):
    for k, v in st["map"].items():
        if v == pool_id:
            return k
    sid = str(st["next"])
    st["next"] += 1
    st["map"][sid] = pool_id
    return sid


def fmt_time(ts):
    if not ts:
        return "时间未知"
    return time.strftime("%Y-%m-%d %H:%M", time.localtime(ts))


def read_args():
    raw = sys.stdin.read()
    try:
        return json.loads(raw) if raw.strip() else {}
    except ValueError:
        return {}


def do_list():
    a = read_args()
    try:
        page = max(1, int(str(a.get("page") or "1")))
    except ValueError:
        page = 1
    st = load_state()
    items = collect()
    touched = False
    msgs = []
    for e in items:
        sid = short_id(st, e["pool_id"])
        touched = True
        outgoing = e["dir"] == "mo"
        msgs.append({
            "id": sid,
            "peer": (e["from"] or "未知号码") if not outgoing else (e["to"] or "未知号码"),
            "dir": "mo" if outgoing else "mt",
            "status": "read" if outgoing or sid in st["read"] else "unread",
            "time": fmt_time(e["ts"]),
            "preview": (e["body"][:40] + ("…" if len(e["body"]) > 40 else "")),
        })
    if touched:
        save_state(st)
    total = len(msgs)
    pages = max(1, (total + PAGE_SIZE - 1) // PAGE_SIZE)
    page = min(page, pages)
    start = (page - 1) * PAGE_SIZE
    return {"ok": True, "msgs": msgs[start:start + PAGE_SIZE], "total": total,
            "pages": pages, "page": page,
            "unread": sum(1 for m in msgs if m["status"] == "unread")}


def do_show():
    a = read_args()
    sid = str(a.get("id") or "")
    st = load_state()
    pool_id = st["map"].get(sid)
    if not pool_id:
        return {"ok": False, "error": "短信不存在"}
    e = parse_entry(os.path.join(POOL, pool_id))
    if not e:
        return {"ok": False, "error": "短信已不可读"}
    peer = e["to"] if e["dir"] == "mo" else e["from"]
    header = "%s · %s · %s" % ("已发送" if e["dir"] == "mo" else "已接收",
                               peer or "未知号码", fmt_time(e["ts"]))
    if e["dir"] != "mo" and sid not in st["read"]:
        st["read"].append(sid)
        save_state(st)
    return {"ok": True, "text": header + "\n---\n" + e["body"]}


def do_send():
    a = read_args()
    num = str(a.get("num") or "").strip()
    text = a.get("text") or ""
    if not num or not text:
        return {"ok": False, "error": "号码和内容不能为空"}
    data = json.dumps({"phone": num, "message": text}).encode()
    req = urllib.request.Request(AGENT + "/api/sms/send", data=data,
                                 headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=40) as r:
            resp = json.loads(r.read().decode("utf-8", "replace"))
    except Exception as e:
        return {"ok": False, "error": "发送未确认: %s" % e}
    if resp.get("sent") or resp.get("ok"):
        return {"ok": True, "started": True}
    return {"ok": False, "error": json.dumps(resp, ensure_ascii=False)[:200]}


def do_job():
    # The Agent sends synchronously, so there is never a queued job to watch.
    return {"ok": True, "none": True}


METHODS = {"sms_list": do_list, "sms_show": do_show, "sms_send": do_send, "sms_job": do_job}

if __name__ == "__main__":
    name = sys.argv[1] if len(sys.argv) > 1 else ""
    fn = METHODS.get(name)
    if not fn:
        print(json.dumps({"error": "unknown_method"}))
        sys.exit(1)
    try:
        print(json.dumps(fn(), ensure_ascii=False))
    except Exception as e:
        print(json.dumps({"error": str(e)}, ensure_ascii=False))
