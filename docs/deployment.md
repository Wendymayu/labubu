# Labubu 部署文档

**服务器:** 101.37.215.110 (阿里云)
**OS:** Alibaba Cloud Linux 3
**服务管理:** systemd

## 目录结构

| 路径 | 说明 |
|------|------|
| `/opt/labubu/` | 项目根目录 |
| `/opt/labubu/bin/labubu` | Go 编译产物(二进制) |
| `/var/lib/labubu/data/` | Trace 持久化数据目录 |
| `/var/lib/labubu/metrics/` | Metrics 持久化数据目录 |
| `/etc/systemd/system/labubu.service` | systemd 服务定义 |

## 日常运维命令

```bash
# 查看服务状态
systemctl status labubu

# 停止 / 启动 / 重启
systemctl stop labubu
systemctl start labubu
systemctl restart labubu

# 查看实时日志
journalctl -u labubu -f

# 查看最近 100 行日志
journalctl -u labubu --no-pager -n 100
```

## 代码更新后重新部署

```bash
# 1. 登录服务器
ssh root@101.37.215.110

# 2. 拉取最新代码
cd /opt/labubu
git checkout develop
git pull origin develop

# 3. 重新构建前端
cd /opt/labubu/web
npm install
npm run build

# 4. 重新编译 Go 二进制(国内需设 GOPROXY)
cd /opt/labubu
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=0 go build -ldflags "-X main.Version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/labubu ./cmd/labubu

# 5. 验证编译产物
bin/labubu version

# 6. 重启服务
systemctl restart labubu

# 7. 检查服务是否正常
systemctl status labubu
curl -s -o /dev/null -w "HTTP %{http_code}\n" http://localhost:8080/
```

## 监听端口

| 端口 | 协议 | 用途 |
|------|------|------|
| 8080 | HTTP | Web UI + REST API |
| 4317 | gRPC | OTLP gRPC 接收 |
| 4318 | HTTP | OTLP HTTP 接收 |

## 服务配置

当前启动参数(见 `/etc/systemd/system/labubu.service`)：

```
labubu serve \
  --port 8080 \
  --data-dir /var/lib/labubu/data \
  --metrics-data-dir /var/lib/labubu/metrics
```

完整参数列表：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--port` | 8080 | Web UI 和 API 端口 |
| `--data-dir` | "" | Trace 数据目录(空=纯内存，重启丢失) |
| `--metrics-data-dir` | "" | Metrics 数据目录(空=纯内存) |
| `--metrics-enabled` | true | 是否启用指标接收 |
| `--buffer-size` | 1000 | 流水线缓冲区大小 |
| `--flush-interval` | 200ms | 流水线刷盘间隔 |
| `--log-level` | info | 日志级别(debug/info/warn/error) |
| `--config` | labubu.yaml | YAML 配置文件路径 |

如需修改参数，编辑 `/etc/systemd/system/labubu.service` 中的 `ExecStart` 行，然后执行：

```bash
systemctl daemon-reload
systemctl restart labubu
```

## 首次部署记录

- **部署日期:** 2026-06-07
- **Go 版本:** 1.25.9 (通过 yum 安装)
- **Node.js 版本:** v22.22.1 (服务器预装)
- **编译环境:** `GOPROXY=https://goproxy.cn,direct` (国内代理，否则无法拉取 Go 模块)
