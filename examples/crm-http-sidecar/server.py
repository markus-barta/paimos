#!/usr/bin/env python3
import hashlib
import hmac
import json
import os
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs, urlparse

SECRET = os.environ.get("PAIMOS_CRM_HMAC_SECRET", "dev-secret")

def json_bytes(obj):
    return json.dumps(obj, separators=(",", ":")).encode("utf-8")

def verify(handler, body):
    ts = handler.headers.get("X-Paimos-Timestamp", "")
    sig = handler.headers.get("X-Paimos-Signature", "")
    if not ts or not sig:
        return False
    try:
        seconds = int(ts)
    except ValueError:
        return False
    if abs(int(time.time()) - seconds) > 300:
        return False
    mac = hmac.new(SECRET.encode("utf-8"), ts.encode("utf-8") + b"\n" + body, hashlib.sha256)
    return hmac.compare_digest(mac.hexdigest(), sig)

class Handler(BaseHTTPRequestHandler):
    def _body(self):
        n = int(self.headers.get("Content-Length", "0"))
        return self.rfile.read(n) if n else b""

    def _send(self, status, obj):
        raw = json_bytes(obj)
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def _reject(self):
        self._send(401, {"error": "unauthorized"})

    def do_GET(self):
        body = b""
        if not verify(self, body):
            return self._reject()
        parsed = urlparse(self.path)
        if parsed.path == "/v1/schema":
            return self._send(200, {
                "version": "crm-http-v1",
                "name": "Example CRM sidecar",
                "capabilities": ["import", "sync", "search", "deep_link"],
            })
        if parsed.path == "/v1/deep-link":
            external_id = parse_qs(parsed.query).get("id", [""])[0]
            return self._send(200, {"url": f"https://crm.example/customers/{external_id}"})
        self._send(404, {"error": "not found"})
    def do_POST(self):
        body = self._body()
        if not verify(self, body):
            return self._reject()
        parsed = urlparse(self.path)
        payload = json.loads(body.decode("utf-8") or "{}")
        if parsed.path == "/v1/import":
            ref = payload.get("ref", "")
            return self._send(200, {
                "name": "Example Customer",
                "industry": "Software",
                "external_id": ref or "example-1",
                "external_url": f"https://crm.example/customers/{ref or 'example-1'}",
                "contacts": [{
                    "name": "Ada Admin",
                    "email": "ada@example.com",
                    "is_primary": True,
                    "external_id": "contact-1",
                }],
            })
        if parsed.path == "/v1/sync":
            external_id = payload.get("external_id", "")
            return self._send(200, {
                "name": "Example Customer",
                "external_url": f"https://crm.example/customers/{external_id}",
            })
        if parsed.path == "/v1/search":
            return self._send(200, {"hits": [{
                "external_id": "example-1",
                "name": "Example Customer",
                "industry": "Software",
                "external_url": "https://crm.example/customers/example-1",
            }]})
        self._send(404, {"error": "not found"})

if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8089"))
    print(f"CRM sidecar listening on http://127.0.0.1:{port}")
    HTTPServer(("127.0.0.1", port), Handler).serve_forever()
