#!/usr/bin/env bash
# start-all.sh — 启动 xiaohongshu-mcp（单进程多租户）+ 辅助 MCP 服务
# 用法: ./start-all.sh [--headless=false]
#
# xiaohongshu-mcp 单进程监听 :18060，通过 URL path 区分 bot：
#   /mcp/bot1  → 使用 cookies-bot1.json + xhs-profiles/bot1/
#   /mcp/bot7  → 使用 cookies-bot7.json + xhs-profiles/bot7/
#   /mcp       → fallback（兼容旧接口）

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
STATUS_FILE="/home/rooot/.openclaw/logs/start-all-status.json"
START_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
echo "{\"status\":\"running\",\"startedAt\":\"${START_TIME}\"}" > "$STATUS_FILE"
BIN="${SCRIPT_DIR}/xiaohongshu-mcp"
LOG_DIR="/tmp"
HEADLESS="${1:---headless=true}"
COMPLIANCE_BIN="/home/rooot/MCP/compliance-mcp/compliance-mcp"
IMAGE_GEN_DIR="/home/rooot/MCP/image-gen-mcp"
XHS_PORT=18060
PROFILES_BASE="/home/rooot/.xhs-profiles"

# 启动 image-gen-mcp（图片生成服务，端口 18085）
OLD_PID=$(lsof -ti:18085 2>/dev/null || true)
if [ -n "$OLD_PID" ]; then
    echo "停止 image-gen-mcp 旧进程 (PID: ${OLD_PID})"
    kill "$OLD_PID" 2>/dev/null || true
    sleep 0.5
fi
if [ -d "$IMAGE_GEN_DIR" ]; then
    echo "启动 image-gen-mcp → :18085"
    setsid nohup python3 "${IMAGE_GEN_DIR}/server.py" --transport streamable-http --port 18085 > "${LOG_DIR}/image-gen-mcp.log" 2>&1 &
else
    echo "警告: image-gen-mcp 目录不存在: ${IMAGE_GEN_DIR}"
fi

# 启动 compliance-mcp（合规审核服务，端口 18090）
OLD_PID=$(lsof -ti:18090 2>/dev/null || true)
if [ -n "$OLD_PID" ]; then
    echo "停止 compliance-mcp 旧进程 (PID: ${OLD_PID})"
    kill "$OLD_PID" 2>/dev/null || true
    sleep 0.5
fi
if [ -x "$COMPLIANCE_BIN" ]; then
    echo "启动 compliance-mcp → :18090"
    setsid nohup "$COMPLIANCE_BIN" -port=:18090 > "${LOG_DIR}/compliance-mcp.log" 2>&1 &
else
    echo "警告: compliance-mcp 二进制不存在: ${COMPLIANCE_BIN}"
fi

# 确保 Xvfb 虚拟显示器在运行（有头模式需要）
if ! pgrep -x Xvfb > /dev/null 2>&1; then
    echo "启动 Xvfb 虚拟显示器..."
    Xvfb :99 -screen 0 1920x1080x24 &>/dev/null &
    sleep 1
fi
export DISPLAY=:99

if [ ! -x "$BIN" ]; then
    echo "二进制文件不存在: $BIN，先编译..."
    cd "$SCRIPT_DIR" && go build -o "$BIN" .
fi

# 停掉占用 XHS 端口的旧进程
OLD_PID=$(lsof -ti:${XHS_PORT} 2>/dev/null || true)
if [ -n "$OLD_PID" ]; then
    echo "停止 xiaohongshu-mcp 旧进程 (PID: ${OLD_PID}, 端口: ${XHS_PORT})"
    kill "$OLD_PID" 2>/dev/null || true
    sleep 1
fi

# 也停掉可能残留的旧多端口进程
pkill -f 'xiaohongshu-mcp.*-port' 2>/dev/null || true
sleep 0.5

# 启动单实例（多租户模式）
echo "启动 xiaohongshu-mcp → :${XHS_PORT} (单进程多租户, profiles: ${PROFILES_BASE})"
nohup "$BIN" ${HEADLESS} -port=:${XHS_PORT} -profiles-base=${PROFILES_BASE} > "${LOG_DIR}/xhs-mcp-unified.log" 2>&1 &
XHS_PID=$!

echo ""
echo "启动完成。等待 3 秒后检查健康..."
sleep 3

OK=0
FAIL=0

# 检查 xiaohongshu-mcp
if curl -sf "http://localhost:${XHS_PORT}/health" > /dev/null 2>&1; then
    echo "  OK  xiaohongshu-mcp :${XHS_PORT} (PID: ${XHS_PID})"
    OK=$((OK + 1))
else
    echo "  FAIL xiaohongshu-mcp :${XHS_PORT}"
    FAIL=$((FAIL + 1))
fi

# 检查 compliance-mcp
if curl -sf "http://localhost:18090/health" > /dev/null 2>&1; then
    echo "  OK  compliance-mcp :18090"
    OK=$((OK + 1))
else
    echo "  FAIL compliance-mcp :18090"
    FAIL=$((FAIL + 1))
fi

# image-gen-mcp 用 MCP initialize 探测（无 /health 端点）
if curl -sf -X POST http://localhost:18085/mcp \
    -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"health","version":"1"}}}' \
    -o /dev/null 2>/dev/null; then
    echo "  OK  image-gen-mcp :18085"
    OK=$((OK + 1))
else
    echo "  FAIL image-gen-mcp :18085"
    FAIL=$((FAIL + 1))
fi

END_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
echo ""
echo "结果: ${OK} 成功, ${FAIL} 失败"
echo "{\"status\":\"$([ $FAIL -eq 0 ] && echo ok || echo partial)\",\"startedAt\":\"${START_TIME}\",\"finishedAt\":\"${END_TIME}\",\"ok\":${OK},\"fail\":${FAIL}}" > "$STATUS_FILE"
