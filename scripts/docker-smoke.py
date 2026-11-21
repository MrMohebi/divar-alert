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


SCHEMA_VERSION = 3


def run_bot(name, image, data, port):
    """Start the image serving both platforms through the mock Bot API."""
    mock = f"http://127.0.0.1:{port}"
    docker("run", "-d", "--name", name, "--network", "host",
           "-e", "TELEGRAM_BOT_TOKEN=123:fake-telegram-token", "-e", f"TELEGRAM_API_URL={mock}",
           "-e", "BALE_BOT_TOKEN=456:fake-bale-token", "-e", f"BALE_API_URL={mock}",
           "-e", "DB_PATH=/root/app/data/divar-alert.db",
           "-v", f"{data}:/root/app/data", image)


def stop_bot(name):
    docker("stop", "--timeout", "60", name)
    state = json.loads(docker("inspect", name))[0]["State"]
    assert state["ExitCode"] == 0, state
    docker("rm", name)


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
            # Validate Compose with dummy configuration, never the developer's secrets.
            (root / "docker-compose.yml").write_text(Path("docker-compose.yml").read_text())
            (root / ".env").write_text("DIVAR_IMAGE_TAG=smoke\nTELEGRAM_BOT_TOKEN=fake-tg\nBALE_BOT_TOKEN=fake-bale\nDB_PATH=./wrong.db\nOPENAI_API_KEY=fake-ai\nOPENAI_BASE_URL=http://mock.invalid/v1\nOPENAI_MODEL=smoke-model\n")
            env = os.environ.copy()
            for key in list(env):
                if key.startswith(("DIVAR_", "TELEGRAM_", "BALE_", "OPENAI_", "COMPOSE_")) or key == "DB_PATH":
                    env.pop(key)
            compose = json.loads(subprocess.check_output(["docker", "compose", "--project-directory", str(root), "config", "--format", "json"], text=True, env=env))
            service = compose["services"]["app"]
            assert service["image"] == "ghcr.io/mrmohebi/divar-alert:smoke"
            assert service["environment"]["TELEGRAM_BOT_TOKEN"] == "fake-tg"
            assert service["environment"]["BALE_BOT_TOKEN"] == "fake-bale"
            assert service["environment"]["OPENAI_API_KEY"] == "fake-ai"
            assert service["environment"]["OPENAI_MODEL"] == "smoke-model"
            assert service["environment"]["OPENAI_BASE_URL"] == "http://mock.invalid/v1"
            assert service["environment"]["DB_PATH"] == "/root/app/data/divar-alert.db"
            assert service["volumes"][0]["target"] == "/root/app/data"
            assert not service.get("ports")
            assert service["stop_grace_period"] == "1m0s"

            # A fresh database is created on first start and kept across
            # container recreation. The host only touches it while stopped.
            data = root / "data"
            data.mkdir()
            path = data / "divar-alert.db"
            for cycle in range(2):
                previous = calls["getupdates"]
                run_bot(name, image, data, server.server_port)
                deadline = time.monotonic() + 20
                while calls["getupdates"] <= previous:
                    state = json.loads(docker("inspect", name))[0]["State"]
                    if not state["Running"]:
                        raise AssertionError(docker("logs", name))
                    if time.monotonic() > deadline:
                        raise AssertionError("Startup timed out: " + docker("logs", name))
                    time.sleep(0.1)
                docker("exec", name, "sh", "-c", "test -s /etc/ssl/certs/ca-certificates.crt && test ! -e /src && test ! -e /root/app/.env && test ! -e /root/app/.git")
                logs = subprocess.run(["docker", "logs", name], capture_output=True, text=True).stderr
                for platform in ("telegram", "bale"):
                    assert f'"msg":"Bot connected","platform":"{platform}"' in logs, logs
                stop_bot(name)
                with sqlite3.connect(path) as conn:
                    assert conn.execute("PRAGMA user_version").fetchone() == (SCHEMA_VERSION,)
                    assert conn.execute("PRAGMA integrity_check").fetchone() == ("ok",)
                    if cycle == 0:
                        conn.execute("INSERT INTO drafts(platform,chat_id,data,updated_at) VALUES('bale',999,'{}',123)")
                    else:
                        assert conn.execute("SELECT data FROM drafts WHERE platform='bale' AND chat_id=999").fetchone() == ("{}",)

            # A database from an earlier version is refused, not migrated.
            old = root / "old"
            old.mkdir()
            with sqlite3.connect(old / "divar-alert.db") as conn:
                conn.execute("CREATE TABLE alerts(id INTEGER PRIMARY KEY, chat_id INTEGER NOT NULL)")
            run_bot(name, image, old, server.server_port)
            deadline = time.monotonic() + 20
            while json.loads(docker("inspect", name))[0]["State"]["Running"]:
                if time.monotonic() > deadline:
                    raise AssertionError("Old database was not refused: " + docker("logs", name))
                time.sleep(0.1)
            state = json.loads(docker("inspect", name))[0]["State"]
            logs = subprocess.run(["docker", "logs", name], capture_output=True, text=True).stderr
            assert state["ExitCode"] != 0 and "start with a new DB_PATH" in logs, logs
            docker("rm", name)

            assert calls["getme"] >= 4 and calls["setmycommands"] >= 4
            assert not (set(calls) - {"getme", "getupdates", "setmycommands"}), calls
            print("PASS: Compose config, Telegram and Bale startup, embedded catalogs, CA bundle, fresh schema, persistent data, old-database refusal, and graceful shutdown; no chat messages.")
    finally:
        subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
