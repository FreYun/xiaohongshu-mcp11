#!/usr/bin/env bash
# start-all.sh — 启动所有 bot 的 xiaohongshu-mcp 实例
# 用法: ./start-all.sh [--headless=false]
#
# 每个 bot 映射:
#   bot1 → :18061, bot2 → :18062, ... bot10 → :18070
#
# 如果 /home/rooot/.openclaw/browser/botN/user-data 存在，
# 则传 --profile-dir 使用 openclaw 内置浏览器 profile。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN="${SCRIPT_DIR}/xiaohongshu-mcp"
BROWSER_BASE="/home/rooot/.openclaw/browser"
LOG_DIR="/tmp"
HEADLESS="${1:---headless=false}"
COMPLIANCE_BIN="/home/rooot/MCP/compliance-mcp/compliance-mcp"

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

for i in $(seq 1 10); do
    PORT=$((18060 + i))
    BOT="bot${i}"
    PROFILE_DIR="${BROWSER_BASE}/${BOT}/user-data"
    LOG_FILE="${LOG_DIR}/xhs-mcp-${BOT}.log"

    # 先杀掉占用该端口的旧进程
    OLD_PID=$(lsof -ti:${PORT} 2>/dev/null || true)
    if [ -n "$OLD_PID" ]; then
        echo "停止 ${BOT} 旧进程 (PID: ${OLD_PID}, 端口: ${PORT})"
        kill "$OLD_PID" 2>/dev/null || true
        sleep 0.5
    fi

    # 构建启动参数
    ARGS="${HEADLESS} -port=:${PORT}"
    if [ -d "$PROFILE_DIR" ]; then
        ARGS="${ARGS} -profile-dir=${PROFILE_DIR}"
        echo "启动 ${BOT} → :${PORT} (profile: ${PROFILE_DIR})"
    else
        echo "启动 ${BOT} → :${PORT} (无 profile，使用 cookie 模式)"
    fi

    # 后台启动
    nohup "$BIN" $ARGS > "$LOG_FILE" 2>&1 &

    # 错开启动，避免同时打开太多 Chrome
    sleep 1
done

echo ""
echo "全部启动完成。等待 3 秒后检查健康..."
sleep 3

OK=0
FAIL=0
for i in $(seq 1 10); do
    PORT=$((18060 + i))
    BOT="bot${i}"
    if curl -sf "http://localhost:${PORT}/health" > /dev/null 2>&1; then
        echo "  OK  ${BOT} :${PORT}"
        ((OK++))
    else
        echo "  FAIL ${BOT} :${PORT}"
        ((FAIL++))
    fi
done

if curl -sf "http://localhost:18090/health" > /dev/null 2>&1; then
    echo "  OK  compliance-mcp :18090"
    ((OK++))
else
    echo "  FAIL compliance-mcp :18090"
    ((FAIL++))
fi

echo ""
echo "结果: ${OK} 成功, ${FAIL} 失败"
