package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
)

// extractBotIDFromPath 从 URL path 提取 botID。
// "/mcp/bot7" → "bot7", "/mcp/bot7/xxx" → "bot7", "/mcp" → ""
func extractBotIDFromPath(path string) string {
	// 去掉 "/mcp/" 前缀
	path = strings.TrimPrefix(path, "/mcp/")
	if path == "" || path == "mcp" {
		return ""
	}
	// 取第一段（botID 后面可能还有 session path 等）
	if idx := strings.Index(path, "/"); idx >= 0 {
		path = path[:idx]
	}
	return path
}

// setupRoutes 设置路由配置
func setupRoutes(appServer *AppServer) *gin.Engine {
	// 设置 Gin 模式
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// 添加中间件
	router.Use(errorHandlingMiddleware())
	router.Use(corsMiddleware())

	// 健康检查
	router.GET("/health", healthHandler)

	// MCP 端点 — 单进程多租户，从 URL path 提取 botID
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			botID := extractBotIDFromPath(r.URL.Path)
			if botID == "" {
				logrus.Debug("MCP request: using default server (no botID)")
				return appServer.defaultMcpServer
			}
			logrus.Debugf("MCP request: routing to bot %s", botID)
			return appServer.getOrCreateBotMCPServer(botID)
		},
		&mcp.StreamableHTTPOptions{
			JSONResponse: true,
		},
	)
	// /mcp — fallback（兼容旧的无 botID 请求）
	router.Any("/mcp", gin.WrapH(mcpHandler))
	// /mcp/:botID — 多租户路由（如 /mcp/bot7）
	// /mcp/:botID/* — 处理 MCP session 子路径
	router.Any("/mcp/*path", gin.WrapH(mcpHandler))

	// API 路由组（暂不改，保持兼容）
	api := router.Group("/api/v1")
	{
		api.GET("/login/status", appServer.checkLoginStatusHandler)
		api.GET("/login/qrcode", appServer.getLoginQrcodeHandler)
		api.DELETE("/login/cookies", appServer.deleteCookiesHandler)
		api.POST("/publish", appServer.publishHandler)
		api.POST("/publish_video", appServer.publishVideoHandler)
		api.GET("/feeds/list", appServer.listFeedsHandler)
		api.GET("/feeds/search", appServer.searchFeedsHandler)
		api.POST("/feeds/search", appServer.searchFeedsHandler)
		api.POST("/feeds/detail", appServer.getFeedDetailHandler)
		api.POST("/user/profile", appServer.userProfileHandler)
		api.POST("/feeds/comment", appServer.postCommentHandler)
		api.POST("/feeds/comment/reply", appServer.replyCommentHandler)
		api.GET("/user/me", appServer.myProfileHandler)
	}

	return router
}
