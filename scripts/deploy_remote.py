#!/usr/bin/env python3
"""Deploy labubu to the Alibaba Cloud server via SSH (password auth).

Reads the root password from the file path in LABUBU_PASS_FILE (default
~/.labubu_pass), runs the documented deploy sequence over a single SSH
session with streaming output, then deletes the password file.
"""
import os
import sys
import stat
import time
import paramiko

HOST = "101.37.215.110"
USER = "root"
PASS_FILE = os.path.expanduser(os.environ.get("LABUBU_PASS_FILE", "~/.labubu_pass"))

# Deploy sequence from docs/deployment.md. `set -e` aborts on first error.
DEPLOY_SCRIPT = r"""
set -euo pipefail
echo "===== [1/7] git pull origin develop ====="
cd /opt/labubu
git checkout develop
git pull origin develop
echo "===== [2/7] npm install (web) ====="
cd /opt/labubu/web
npm install
echo "===== [3/7] npm run build (web) ====="
npm run build
echo "===== [4/7] go build (CGO_ENABLED=0) ====="
cd /opt/labubu
export GOPROXY=https://goproxy.cn,direct
export CGO_ENABLED=0
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
go build -ldflags "-X main.Version=$VERSION" -o bin/labubu ./cmd/labubu
echo "===== [5/7] verify binary ====="
bin/labubu version
echo "===== [6/7] systemctl restart labubu ====="
systemctl restart labubu
sleep 2
echo "===== [7/7] health check ====="
systemctl status labubu --no-pager -l | head -20
curl -s -o /dev/null -w "HTTP %{http_code}\n" http://localhost:8080/
echo "===== DEPLOY COMPLETE ====="
"""


def read_password():
    if not os.path.exists(PASS_FILE):
        sys.exit(f"password file not found: {PASS_FILE}. Create it first.")
    with open(PASS_FILE, "r") as f:
        return f.read().strip()


def main():
    password = read_password()
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    print(f"connecting to {USER}@{HOST} ...", flush=True)
    client.connect(HOST, username=USER, password=password, timeout=20)
    print("connected. running deploy sequence...\n", flush=True)

    transport = client.get_transport()
    channel = transport.open_session()
    channel.set_combine_stderr(True)
    channel.exec_command("bash -l -c " + paramiko.util.shlex.quote(DEPLOY_SCRIPT))

    start = time.time()
    while True:
        if channel.recv_ready():
            data = channel.recv(4096).decode("utf-8", "replace")
            sys.stdout.write(data)
            sys.stdout.flush()
        if channel.exit_status_ready():
            # drain remaining
            while channel.recv_ready():
                data = channel.recv(4096).decode("utf-8", "replace")
                sys.stdout.write(data)
            break
        time.sleep(0.2)

    rc = channel.recv_exit_status()
    elapsed = time.time() - start
    print(f"\n--- remote exit status: {rc} ({elapsed:.1f}s) ---", flush=True)
    client.close()

    # always wipe the password file regardless of outcome
    try:
        os.remove(PASS_FILE)
        print(f"deleted password file {PASS_FILE}", flush=True)
    except OSError as e:
        print(f"WARNING: could not delete {PASS_FILE}: {e}", flush=True)

    sys.exit(rc)


if __name__ == "__main__":
    main()
