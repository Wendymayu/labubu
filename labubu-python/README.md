# Labubu

Local-first LLM observability platform.

## Installation

### From GitHub Release (recommended)

Install the wheel matching your platform directly from GitHub Releases:

```bash
# Linux x86_64
pip install https://github.com/Wendymayu/labubu/releases/download/v0.1.0/labubu-0.1.0-py3-none-linux_x86_64.whl

# Linux ARM64
pip install https://github.com/Wendymayu/labubu/releases/download/v0.1.0/labubu-0.1.0-py3-none-linux_aarch64.whl

# macOS Intel
pip install https://github.com/Wendymayu/labubu/releases/download/v0.1.0/labubu-0.1.0-py3-none-macosx_10_9_x86_64.whl

# macOS Apple Silicon
pip install https://github.com/Wendymayu/labubu/releases/download/v0.1.0/labubu-0.1.0-py3-none-macosx_11_0_arm64.whl

# Windows
pip install https://github.com/Wendymayu/labubu/releases/download/v0.1.0/labubu-0.1.0-py3-none-win_amd64.whl
```

Replace `v0.1.0` with the desired version. Available releases: [github.com/Wendymayu/labubu/releases](https://github.com/Wendymayu/labubu/releases)

### From PyPI (when available)

```bash
pip install labubu
```

### Standalone Binary

Download the raw binary for your platform from the [Releases page](https://github.com/Wendymayu/labubu/releases):

| Platform | File |
|----------|------|
| Linux x86_64 | `labubu-linux-amd64` |
| Linux ARM64 | `labubu-linux-arm64` |
| macOS Intel | `labubu-darwin-amd64` |
| macOS Apple Silicon | `labubu-darwin-arm64` |
| Windows | `labubu-windows-amd64.exe` |

## Quick Start

```bash
labubu serve
```

Then open [http://localhost:8080](http://localhost:8080) for the UI.

Configure your agent to send OTLP traces to `http://localhost:4318`.

## Commands

| Command | Description |
|---------|-------------|
| `labubu serve` | Start the server (default port 8080) |
| `labubu version` | Print version information |
| `labubu help` | Show help |

### Serve Options

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | 8080 | API & UI listen port |
| `--log-level` | info | Log level: debug, info, warn, error |
| `--buffer-size` | 1000 | Pipeline buffer capacity |
| `--flush-interval` | 200ms | Pipeline flush interval |

## Example

```bash
labubu serve --port 9090 --log-level debug
```
