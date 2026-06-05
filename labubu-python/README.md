# Labubu

Local-first LLM observability platform.

## Quick Start

```bash
pip install labubu
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
