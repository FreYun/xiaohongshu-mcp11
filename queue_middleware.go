package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const (
	queueTimeout = 4 * time.Minute // 排队超时，与 browserQueueTimeout 一致
)

// mcpToolQueue 对 MCP tools/call 请求做 per-bot 串行化。
// 每个 bot 同一时间只允许 1 个 tools/call 进入 MCP handler，
// 其余请求（initialize、notifications、GET、DELETE 等）直接放行。
type mcpToolQueue struct {
	mu    sync.Mutex
	lanes map[string]chan struct{} // botID → 容量 1 的 channel（互斥令牌）
}

func newMCPToolQueue() *mcpToolQueue {
	return &mcpToolQueue{
		lanes: make(map[string]chan struct{}),
	}
}

// getLane 获取指定 bot 的排队 channel。
func (q *mcpToolQueue) getLane(botID string) chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	ch, ok := q.lanes[botID]
	if !ok {
		ch = make(chan struct{}, 1)
		q.lanes[botID] = ch
	}
	return ch
}

// middleware 返回 Gin 中间件。
func (q *mcpToolQueue) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 只拦截 POST 请求
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		// 提取 botID
		botID := extractBotIDFromPath(c.Request.URL.Path)
		if botID == "" {
			c.Next()
			return
		}

		// 读取 body 判断是否为 tools/call
		rawBody, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Next()
			return
		}
		// 回填 body，无论是否需要排队，下游都需要读取
		c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))

		if !isToolCallRequest(rawBody) {
			c.Next()
			return
		}

		// === 需要排队 ===
		lane := q.getLane(botID)

		logrus.Infof("[queue] %s: tools/call 排队中（当前队列深度: %d）", botID, len(lane))

		select {
		case lane <- struct{}{}:
			// 获得令牌
		case <-time.After(queueTimeout):
			logrus.Warnf("[queue] %s: 排队超时（%s）", botID, queueTimeout)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"jsonrpc": "2.0",
				"error": gin.H{
					"code":    -32000,
					"message": "tools/call 排队超时，当前系统繁忙，请稍后重试",
				},
			})
			c.Abort()
			return
		case <-c.Request.Context().Done():
			logrus.Infof("[queue] %s: 客户端在排队期间断开", botID)
			c.Abort()
			return
		}

		logrus.Infof("[queue] %s: 开始执行 tools/call", botID)
		start := time.Now()

		defer func() {
			<-lane // 释放令牌
			logrus.Infof("[queue] %s: tools/call 完成，耗时 %s", botID, time.Since(start).Round(time.Millisecond))
		}()

		c.Next()
	}
}

// jsonRPCMessage 用于解析 JSON-RPC 请求的 method 字段。
type jsonRPCMessage struct {
	Method string `json:"method"`
}

// isToolCallRequest 检查 raw body 是否包含 method="tools/call" 的 JSON-RPC 请求。
// 支持单个请求和批量请求（JSON 数组）。
func isToolCallRequest(body []byte) bool {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return false
	}

	// 尝试单个请求
	if body[0] == '{' {
		var msg jsonRPCMessage
		if json.Unmarshal(body, &msg) == nil {
			return msg.Method == "tools/call"
		}
		return false
	}

	// 尝试批量请求（JSON 数组）
	if body[0] == '[' {
		var msgs []jsonRPCMessage
		if json.Unmarshal(body, &msgs) == nil {
			for _, msg := range msgs {
				if msg.Method == "tools/call" {
					return true
				}
			}
		}
	}

	return false
}
