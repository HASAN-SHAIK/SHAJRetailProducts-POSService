#!/usr/bin/env python3
import json
import pathlib
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

port = int(sys.argv[1])
phase_file = pathlib.Path(sys.argv[2])
request_log = pathlib.Path(sys.argv[3])


def phase():
    return phase_file.read_text(encoding="utf-8").strip()


def log_record(record):
    with request_log.open("a", encoding="utf-8") as fh:
        fh.write(json.dumps(record, sort_keys=True) + "\n")


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def send_json(self, status, body, headers=None):
        raw = json.dumps(body).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        for key, value in (headers or {}).items():
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(raw)

    def machine_auth_ok(self):
        return (
            self.headers.get("X-POS-Tenant-ID") == "tenant-customer-restart"
            and self.headers.get("X-POS-Device-ID") == "device-customer-restart"
            and self.headers.get("X-POS-Sync-Token") == "sync-token-customer-restart-valid"
        )

    def do_GET(self):
        parsed = urlparse(self.path)
        if parsed.path == "/probe":
            self.send_json(200, {"ok": True})
            return
        if not self.machine_auth_ok():
            self.send_json(401, {"error": "bad machine identity"})
            return
        if parsed.path == "/api/v1/sync/config/effective":
            self.send_json(200, {
                "schema_version": 1,
                "etag": "customer-restart-etag",
                "scope": {
                    "tenant_id": "tenant-customer-restart",
                    "branch_id": "branch-customer-restart",
                    "device_id": "device-customer-restart"
                },
                "values": {},
                "config": {}
            }, {"ETag": "customer-restart-etag"})
            return
        if parsed.path == "/api/v1/sync/changes":
            self.send_json(200, {"cursor": "", "has_more": False, "changes": []})
            return
        self.send_json(404, {"error": "not found"})

    def do_POST(self):
        parsed = urlparse(self.path)
        if parsed.path != "/api/v1/sync/events":
            self.send_json(404, {"error": "not found"})
            return
        length = int(self.headers.get("Content-Length", "0") or "0")
        raw = self.rfile.read(length) if length else b"{}"
        try:
            body = json.loads(raw.decode("utf-8"))
        except Exception:
            self.send_json(400, {"error": "invalid json"})
            return
        current = phase()
        log_record({
            "phase": current,
            "event_id": body.get("event_id"),
            "event_type": body.get("event_type"),
            "aggregate_type": body.get("aggregate_type"),
            "aggregate_id": body.get("aggregate_id"),
            "aggregate_version": body.get("aggregate_version"),
            "ordering_key": body.get("ordering_key"),
            "idempotency_key": self.headers.get("Idempotency-Key"),
            "auth_ok": self.machine_auth_ok()
        })
        if not self.machine_auth_ok():
            self.send_json(401, {"error": "bad machine identity"})
            return
        if current == "1":
            self.send_json(503, {"error": "central intentionally unavailable before POS restart"})
            return
        self.send_json(200, {"success": True})


ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
