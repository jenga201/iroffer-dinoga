#!/usr/bin/python3
"""Minimal IRC-acceptance probe: connect over TLS, NICK/USER, PONG, and report
whether the IRC server welcomes (0), bans via DNSBL/Akill (1), or we couldn't
determine (2). Used by the dinoga region supervisor to detect a banned VPN exit.
"""
import socket
import ssl
import sys
import time

host = sys.argv[1] if len(sys.argv) > 1 else "irc.rizon.net"
try:
    port = int(sys.argv[2]) if len(sys.argv) > 2 else 9999
    window = int(sys.argv[3]) if len(sys.argv) > 3 else 12
except ValueError:
    sys.exit(2)

nick = "dnprobe%d" % (int(time.time()) % 1000000)
welcomed = False
banned = False

try:
    ctx = ssl.create_default_context()
    sock = ctx.wrap_socket(
        socket.create_connection((host, port), timeout=10),
        server_hostname=host,
    )
except Exception:
    sys.exit(2)

try:
    sock.sendall(("NICK %s\r\nUSER dnprobe 0 * :dinoga probe\r\n" % nick).encode())
    sock.settimeout(2)
    buf = b""
    start = time.time()
    while time.time() - start < window:
        try:
            data = sock.recv(4096)
        except socket.timeout:
            continue
        except Exception:
            break
        if not data:
            break
        buf += data
        while b"\r\n" in buf:
            line, buf = buf.split(b"\r\n", 1)
            decoded = line.decode(errors="replace")
            parts = line.split()
            if parts and parts[0].upper() == b"PING":
                sock.sendall(b"PONG " + parts[1] + b"\r\n")
            low = decoded.lower()
            if " 001 " in decoded:
                welcomed = True
            if any(k in low for k in ("banned", "dnsbl", "blacklisted", "closing link", "akill")):
                banned = True
finally:
    sock.close()

if banned:
    sys.exit(1)
if welcomed:
    sys.exit(0)
sys.exit(2)