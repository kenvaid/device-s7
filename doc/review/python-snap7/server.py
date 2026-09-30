"""Local python-snap7 fixture with a read-only, independent memory observer."""

import argparse
import base64
import importlib.metadata
import json
import logging
import signal
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from s7.server import Server
from s7.type import SrvArea


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=1102)
    parser.add_argument("--observer-port", type=int, default=1103)
    parser.add_argument("--mode", choices=("upstream", "typed"), default="upstream")
    args = parser.parse_args()
    logging.basicConfig(level=logging.WARNING)
    if args.mode == "typed":
        from typed_server import TypedServer
        server = TypedServer(log=False, max_clients=64)
    else:
        server = Server(log=False, max_clients=64)
    regions = {}
    specifications = [
        ("M", SrvArea.MK, 0, 8192),
        ("I", SrvArea.PE, 0, 8192),
        ("Q", SrvArea.PA, 0, 8192),
        ("CT", SrvArea.CT, 0, 2048),
        ("TM", SrvArea.TM, 0, 2048),
        ("DB1", SrvArea.DB, 1, 8192),
        ("DB2", SrvArea.DB, 2, 8192),
        ("DB3", SrvArea.DB, 3, 8192),
    ]
    for ordinal, (name, area, index, size) in enumerate(specifications):
        data = bytearray((i * 17 + ordinal * 31) % 256 for i in range(size))
        server.register_area(area, index, data)
        regions[name] = data

    done = threading.Event()

    class Observer(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path == "/health":
                result = {"version": importlib.metadata.version("python-snap7"),
                          "mode": args.mode, "areas": {n: len(b) for n, b in regions.items()}}
            elif self.path == "/memory":
                result = {n: base64.b64encode(b).decode("ascii") for n, b in regions.items()}
            else:
                self.send_error(404)
                return
            payload = json.dumps(result).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def do_POST(self):
            if self.path != "/shutdown":
                self.send_error(404)
                return
            self.send_response(204)
            self.end_headers()
            done.set()

        def log_message(self, *_):
            pass

    http = ThreadingHTTPServer(("127.0.0.1", args.observer_port), Observer)
    signal.signal(signal.SIGINT, lambda *_: done.set())
    signal.signal(signal.SIGTERM, lambda *_: done.set())
    thread = None
    try:
        server.start_to("127.0.0.1", args.port)
        thread = threading.Thread(target=http.serve_forever, daemon=True)
        thread.start()
        print(json.dumps({"ready": True, "mode": args.mode, "s7": f"127.0.0.1:{args.port}",
                          "observer": f"http://127.0.0.1:{args.observer_port}"}), flush=True)
        done.wait()
    finally:
        if thread is not None:
            http.shutdown()
        http.server_close()
        server.stop()


if __name__ == "__main__":
    main()
