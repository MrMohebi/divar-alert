#!/usr/bin/env python3
"""Exercise a built image with a local fake bot API and disposable SQLite data.

Usage: python3 scripts/docker-smoke.py divar-alert:deployment-check
Requires Linux Docker (the mock API uses host networking). No real credentials.
"""
import collections
import http.server
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import uuid


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True).strip()


calls = collections.Counter()


class MockBot(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.respond()

    def do_GET(self):
        self.respond()

    def respond(self):
        self.rfile.read(int(self.headers.get("Content-Length", "0")))
        method = self.path.split("?")[0].rsplit("/", 1)[-1].lower()
        calls[method] += 1
        if method == "getme":
            result = {"id": 123, "is_bot": True, "first_name": "Smoke", "username": "smoke_bot"}
        elif method == "getupdates":
            time.sleep(0.1)
            result = []
        elif method == "setmycommands":
            result = True
        else:
            self.send_error(400, "Unexpected API method")
            return
        body = json.dumps({"ok": True, "result": result}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def log_message(self, *args):
        pass


def main():
    image = sys.argv[1] if len(sys.argv) > 1 else "divar-alert:deployment-check"
    metadata = json.loads(docker("image", "inspect", image))[0]
    assert (metadata["Os"], metadata["Architecture"]) == ("linux", "amd64")
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), MockBot)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    name = "divar-smoke-" + uuid.uuid4().hex[:10]
    try:
        with tempfile.TemporaryDirectory(prefix="divar-smoke-") as folder:
            root = Path(folder)
            data = root / "data"
            data.mkdir()
            path = data / "divar-alert.db"
            with sqlite3.connect(path) as conn:
                conn.executescript("""
                    CREATE TABLE alerts(id INTEGER PRIMARY KEY,chat_id INTEGER NOT NULL,title TEXT NOT NULL,link TEXT NOT NULL,ai_enabled BOOLEAN NOT NULL DEFAULT true,analysis_prompt TEXT NOT NULL DEFAULT '',interval_seconds INTEGER NOT NULL,last_time_checked INTEGER NOT NULL);
                    CREATE TABLE seen_posts(token TEXT NOT NULL,alert_id INTEGER NOT NULL,data TEXT,created_at INTEGER NOT NULL,PRIMARY KEY(token,alert_id),FOREIGN KEY(alert_id) REFERENCES alerts(id) ON DELETE CASCADE);
                    INSERT INTO seen_posts VALUES('existing',1,'{"title":"retained"}',123);
                """)
                conn.execute("INSERT INTO alerts VALUES(1,42,'smoke','curl',false,'',60,?)", (int(time.time()) + 31536000,))
            # Validate Compose with dummy configuration, never the developer's secrets.
            (root / "docker-compose.yml").write_text(Path("docker-compose.yml").read_text())
            (root / ".env").write_text("DIVAR_IMAGE_TAG=smoke\nTELEGRAM_BOT_TOKEN=fake\nTELEGRAM_API_URL=http://mock.invalid\nDB_PATH=./wrong.db\nOPENAI_API_KEY=fake-ai\nOPENAI_BASE_URL=http://mock.invalid/v1\nOPENAI_MODEL=smoke-model\n")
            env = os.environ.copy()
            for key in list(env):
                if key.startswith(("DIVAR_", "TELEGRAM_", "OPENAI_", "COMPOSE_")) or key == "DB_PATH":
                    env.pop(key)
            compose = json.loads(subprocess.check_output(["docker", "compose", "--project-directory", str(root), "config", "--format", "json"], text=True, env=env))
            service = compose["services"]["app"]
            assert service["image"] == "ghcr.io/mrmohebi/divar-alert:smoke"
            assert service["environment"]["TELEGRAM_BOT_TOKEN"] == "fake"
            assert service["environment"]["OPENAI_API_KEY"] == "fake-ai"
            assert service["environment"]["OPENAI_MODEL"] == "smoke-model"
            assert service["environment"]["OPENAI_BASE_URL"] == "http://mock.invalid/v1"
            assert service["environment"]["DB_PATH"] == "/root/app/data/divar-alert.db"
            assert service["volumes"][0]["target"] == "/root/app/data"
            assert not service.get("ports")
            assert service["stop_grace_period"] == "1m0s"
            old_id = None
            for cycle in range(2):
                previous = calls["getupdates"]
                docker("run", "-d", "--name", name, "--network", "host",
                       "-e", "TELEGRAM_BOT_TOKEN=123:fake-smoke-token",
                       "-e", f"TELEGRAM_API_URL=http://127.0.0.1:{server.server_port}",
                       "-e", "DB_PATH=/root/app/data/divar-alert.db",
                       "-v", f"{data}:/root/app/data", image)
                deadline = time.monotonic() + 20
                while calls["getupdates"] <= previous:
                    state = json.loads(docker("inspect", name))[0]["State"]
                    if not state["Running"]:
                        raise AssertionError(docker("logs", name))
                    if time.monotonic() > deadline:
                        raise AssertionError("Startup timed out: " + docker("logs", name))
                    time.sleep(0.1)
                docker("exec", name, "sh", "-c", "test -s /etc/ssl/certs/ca-certificates.crt && test ! -e /src && test ! -e /root/app/.env && test ! -e /root/app/.git")
                with sqlite3.connect(path) as conn:
                    row = conn.execute("SELECT id,created_at,data,deleted_at FROM seen_posts WHERE token='existing'").fetchone()
                    assert row and row[1:] == (123, '{"title":"retained"}', None)
                    if cycle == 0:
                        old_id = row[0]
                        conn.execute("INSERT INTO processes VALUES(999,'SMOKE','{}',123)")
                    else:
                        assert row[0] == old_id
                        assert conn.execute("SELECT process_key FROM processes WHERE chat_id=999").fetchone() == ("SMOKE",)
                    assert conn.execute("PRAGMA integrity_check").fetchone() == ("ok",)
                docker("stop", "--timeout", "60", name)
                state = json.loads(docker("inspect", name))[0]["State"]
                assert state["ExitCode"] == 0, state
                docker("rm", name)
            assert calls["getme"] >= 2 and calls["setmycommands"] >= 2
            assert not (set(calls) - {"getme", "getupdates", "setmycommands"}), calls
            print("PASS: Compose config, startup, embedded catalogs, CA bundle, legacy migration, persistent data, and graceful shutdown across recreation; no chat messages.")
    finally:
        subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
