#!/usr/bin/env python3
"""/usr/libexec/rpcd/mu300dash backend for the stock AirSIM LuCI SMS panel.

The panel expects a `mu300dash` ubus object with sms_list / sms_show /
sms_send / sms_job. On the VIPME-M8 there is no vendor dash daemon, but the
firmware does ship the stock two-SIM SMS tool /usr/libexec/e5-sms, which already
records which card every message came from and can submit from a chosen card
without switching the data SIM. This helper serves the same contract on top of
it, and adds the card selector the panel never had.

Arguments: <method>, JSON args on stdin, one JSON object on stdout.
"""
import json
import os
import subprocess
import sys
import tempfile
import time

E5 = "/usr/libexec/e5-sms"
INFO = "/tmp/call-forward.info"
PROFILE = "/etc/unisoc-modem/sim%d/sim.json"
STATE = "/opt/airsim-m8/data/luci-sms.json"
PAGE_SIZE = 10


def e5(*argv, timeout=40):
    p = subprocess.run([E5] + list(argv), capture_output=True, text=True, timeout=timeout)
    try:
        value = json.loads(p.stdout)
    except ValueError:
        raise RuntimeError((p.stderr or p.stdout or "e5-sms returned nothing").strip()[:160])
    if value.get("ok") is False:
        raise RuntimeError(value.get("error") or "SMS operation failed")
    return value


def messages():
    return e5("list").get("messages") or []


def state():
    try:
        with open(STATE, "r", encoding="utf-8") as f:
            d = json.load(f)
            d.setdefault("read", [])
            return d
    except Exception:
        return {"read": []}


def save_state(st):
    try:
        os.makedirs(os.path.dirname(STATE), exist_ok=True)
        with open(STATE + ".new", "w", encoding="utf-8") as f:
            json.dump(st, f)
        os.replace(STATE + ".new", STATE)
    except Exception:
        pass


def card_of(m):
    c = m.get("card")
    return "" if c is None else str(c)


def fmt_time(iso):
    t = (iso or "").replace("T", " ")
    return t[:16] if t else "时间未知"


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
    want = str(a.get("card") if a.get("card") is not None else "")
    items = messages()
    if want in ("0", "1"):
        items = [m for m in items if card_of(m) == want]
    st = state()
    msgs = []
    for m in items:
        outgoing = (m.get("direction") or "in") == "out"
        sid = str(m.get("id"))
        body = m.get("text") or ""
        msgs.append({
            "id": sid,
            "peer": m.get("number") or "未知号码",
            "dir": "mo" if outgoing else "mt",
            "status": "read" if outgoing or sid in st["read"] or not m.get("unread") else "unread",
            "time": fmt_time(m.get("time")),
            "preview": body[:40] + ("…" if len(body) > 40 else ""),
            "card": card_of(m),
            "sim": m.get("sim") or "",
            "state": m.get("state") or "",
        })
    total = len(msgs)
    pages = max(1, (total + PAGE_SIZE - 1) // PAGE_SIZE)
    page = min(page, pages)
    start = (page - 1) * PAGE_SIZE
    return {"ok": True, "msgs": msgs[start:start + PAGE_SIZE], "total": total,
            "pages": pages, "page": page,
            "unread": sum(1 for m in msgs if m["status"] == "unread")}


def find(sid):
    for m in messages():
        if str(m.get("id")) == sid:
            return m
    return None


def do_show():
    a = read_args()
    sid = str(a.get("id") or "")
    m = find(sid)
    if not m:
        return {"ok": False, "error": "短信不存在"}
    outgoing = (m.get("direction") or "in") == "out"
    header = "%s · %s · %s · %s" % (
        "已发送" if outgoing else "已接收",
        m.get("number") or "未知号码",
        fmt_time(m.get("time")),
        m.get("sim") or ("SIM%s" % (int(card_of(m) or 0) + 1) if card_of(m) else "来源未知"))
    st = state()
    if not outgoing and sid not in st["read"]:
        st["read"].append(sid)
        st["read"] = st["read"][-500:]
        save_state(st)
    return {"ok": True, "text": header + "\n---\n" + (m.get("text") or "")}


def do_send():
    a = read_args()
    num = str(a.get("num") or "").strip()
    text = a.get("text") or ""
    card = str(a.get("card") if a.get("card") is not None else "")
    if card not in ("0", "1"):
        card = ""
    if not num or not text:
        return {"ok": False, "error": "号码和内容不能为空"}
    fd, path = tempfile.mkstemp(prefix="airsim-sms-")
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="") as f:
            f.write(text)
        argv = ["send", num, path]
        if card:
            argv.append(card)
        out = e5(*argv, timeout=60)
    finally:
        try:
            os.unlink(path)
        except OSError:
            pass
    if out.get("ok") is False:
        return {"ok": False, "error": out.get("error") or "发送失败"}
    return {"ok": True, "started": True, "card": card or "current"}


def intl_number(value):
    """CNUM 的格式看卡：+8613… / 8613… / 裸的 11 位本地号，统一补成国际格式。"""
    value = (value or "").strip()
    if not value:
        return ""
    if value.startswith("+"):
        return value
    if value.startswith("86") and len(value) >= 12:
        return "+" + value
    if value.startswith("1") and len(value) == 11:
        return "+86" + value
    return value


def do_sims():
    """Both cards: which are present, their numbers, operator and ICCID."""
    operators = {
        "46000": "中国移动", "46002": "中国移动", "46004": "中国移动",
        "46007": "中国移动", "46008": "中国移动",
        "46001": "中国联通", "46006": "中国联通", "46009": "中国联通",
        "46003": "中国电信", "46005": "中国电信", "46011": "中国电信",
        "46012": "中国电信", "46015": "中国广电", "46020": "中国铁通",
    }
    info = {}
    try:
        with open(INFO, "r", encoding="utf-8") as f:
            for line in f:
                if "=" in line:
                    k, v = line.split("=", 1)
                    info[k.strip()] = v.strip().strip("'")
    except Exception:
        pass
    slots = (info.get("INFO_SLOTS") or "").split()
    sims = []
    for slot in ("0", "1"):
        present = slot in slots
        entry = {"card": int(slot), "name": "SIM%d" % (int(slot) + 1), "present": present,
                 "number": intl_number(info.get("INFO_SELF_%s" % slot, "")), "operator": "", "iccid": ""}
        try:
            with open(PROFILE % int(slot), "r", encoding="utf-8") as f:
                p = json.load(f)
                entry["iccid"] = p.get("iccid") or ""
                entry["imsi"] = p.get("imsi") or ""
        except Exception:
            pass
        if present:
            # INFO_OPERATOR is one value taken from the data card, so it would
            # label both trays "China Unicom". The IMSI prefix is the only
            # per-tray operator fact available without another AT round trip.
            entry["operator"] = operators.get((entry.get("imsi") or "")[:5]) or info.get("INFO_OPERATOR") or ""
        sims.append(entry)
    current = 0
    try:
        with open("/sys/module/sipc_wwan/parameters/card") as f:
            current = int((f.read().strip() or "0"))
    except Exception:
        current = 0
    return {"ok": True, "sims": sims, "current": current,
            "info_age": int(time.time()) - int(info.get("INFO_TS") or 0) if info.get("INFO_TS") else -1}


def do_job():
    # e5-sms submits synchronously, so there is never a queued job to watch.
    return {"ok": True, "none": True}


METHODS = {"sms_list": do_list, "sms_show": do_show, "sms_send": do_send,
           "sms_job": do_job, "sms_sims": do_sims}

if __name__ == "__main__":
    name = sys.argv[1] if len(sys.argv) > 1 else ""
    fn = METHODS.get(name)
    if not fn:
        print(json.dumps({"error": "unknown_method"}))
        sys.exit(1)
    try:
        print(json.dumps(fn(), ensure_ascii=False))
    except Exception as e:
        print(json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False))
