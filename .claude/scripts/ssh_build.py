import paramiko

host = "101.37.215.110"
user = "root"
password = "52wendyma&hhh"

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())

try:
    client.connect(hostname=host, username=user, password=password, timeout=15)

    # Clone repo from GitHub (try HTTPS first, then SSH)
    cmds = """
set -e
cd /opt
if [ -d labubu ]; then
    echo "=== Repo exists, pulling latest ==="
    cd labubu
    git checkout develop
    git pull origin develop
else
    echo "=== Cloning repo ==="
    git clone https://github.com/Wendymayu/labubu.git
    cd labubu
    git checkout develop
fi
echo "=== BUILDING FRONTEND ==="
cd /opt/labubu/web
npm install
npm run build
echo "=== BUILDING GO ==="
cd /opt/labubu
CGO_ENABLED=0 go build -ldflags "-X main.Version=$(git describe --tags --always --dirty 2>/dev/null || echo 'v1.0.0')" -o bin/labubu ./cmd/labubu
echo "=== BUILD DONE ==="
ls -lh /opt/labubu/bin/labubu
/opt/labubu/bin/labubu version
"""
    stdin, stdout, stderr = client.exec_command(cmds)

    # Stream output
    while not stdout.channel.exit_status_ready:
        if stdout.channel.recv_ready():
            data = stdout.channel.recv(4096).decode('utf-8')
            print(data, end='', flush=True)
        if stderr.channel.recv_stderr_ready():
            err = stderr.channel.recv_stderr(4096).decode('utf-8')
            print(err, end='', flush=True)

    # Get remaining data
    print(stdout.read().decode('utf-8'), end='', flush=True)
    err_remain = stderr.read().decode('utf-8')
    if err_remain.strip():
        print("STDERR:", err_remain)

    client.close()
except Exception as e:
    print(f"ERROR: {e}")
