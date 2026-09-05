#!/usr/bin/env python3
import json
import pathlib
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

port = int(sys.argv[1])
phase_file = pathlib.Path(sys.argv[2])
request_log = pathlib.Path(sys.argv[3])


def phase():
    return phase_file.read_text().strip()


def append_log(line):
    with request_log.open("a", encoding="utf-8") as fh:
        fh.write(line + "\n")


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def _json(self, status, body, headers=None):
        payload = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        for key, value in (headers or {}).items():
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        parsed = urlparse(self.path)
        current = phase()
        tenant = self.headers.get("X-POS-Tenant-ID", "")
        device = self.headers.get("X-POS-Device-ID", "")
        token = self.headers.get("X-POS-Sync-Token", "")
        if tenant != "tenant-cycle-c" or device != "device-cycle-c" or token != "sync-token-cycle-c-valid":
            append_log(f"phase={current} auth=bad path={parsed.path} tenant={tenant} device={device}")
            self._json(401, {"error": "bad machine identity"})
            return

        if parsed.path == "/api/v1/sync/config/effective":
            etag = "etag-1" if current == "1" else "etag-2"
            append_log(f"phase={current} config etag={etag}")
            self._json(200, {
                "schema_version": 1,
                "etag": etag,
                "scope": {"tenant_id": "tenant-cycle-c", "branch_id": "branch-cycle-c", "device_id": "device-cycle-c"},
                "values": {"offline.sales_enabled": current == "1"},
                "config": {"offline": {"sales_enabled": current == "1"}}
            }, {"ETag": etag})
            return

        if parsed.path == "/api/v1/sync/changes":
            cursor = parse_qs(parsed.query).get("cursor", [""])[0]
            append_log(f"phase={current} changes cursor={cursor or '<empty>'}")
            if current == "1":
                if cursor == "":
                    self._json(200, {
                        "cursor": "cursor-1",
                        "has_more": False,
                        "changes": [{
                            "id": "product:cycle-c:v1",
                            "type": "catalog.product.upsert",
                            "schema_version": 1,
                            "source": "central",
                            "payload": {"id": "cycle-c-product", "name": "Restart Milk V1", "is_active": True, "allow_manual_price": False, "track_inventory": True, "version": 1}
                        }]
                    })
                else:
                    self._json(200, {"cursor": "cursor-1", "has_more": False, "changes": []})
                return

            if cursor == "cursor-1":
                self._json(200, {
                    "cursor": "cursor-2",
                    "has_more": False,
                    "changes": [{
                        "id": "product:cycle-c:v2",
                        "type": "catalog.product.upsert",
                        "schema_version": 1,
                        "source": "central",
                        "payload": {"id": "cycle-c-product", "name": "Restart Milk V2", "is_active": True, "allow_manual_price": False, "track_inventory": True, "version": 2}
                    }]
                })
            elif cursor == "cursor-2":
                self._json(200, {"cursor": "cursor-2", "has_more": False, "changes": []})
            else:
                append_log(f"phase={current} unexpected_cursor={cursor or '<empty>'}")
                self._json(409, {"error": "restart did not resume from retained cursor"})
            return

        self._json(404, {"error": "not found"})

    def do_POST(self):
        # Outbound outbox is not under test. Accept an empty event stream so the
        # real POS process can run all production background workers normally.
        parsed = urlparse(self.path)
        if parsed.path == "/api/v1/sync/events":
            length = int(self.headers.get("Content-Length", "0") or "0")
            if length:
                self.rfile.read(length)
            self._json(200, {"success": True})
            return
        self._json(404, {"error": "not found"})


ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
