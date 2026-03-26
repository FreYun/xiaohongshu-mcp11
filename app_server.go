package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
)

// AppServer 应用服务器结构体，封装所有服务和处理器
type AppServer struct {
	xiaohongshuService *XiaohongshuService
	defaultMcpServer   *mcp.Server            // fallback（无 botID 时使用）
	botMcpServers      map[string]*mcp.Server  // botID → per-bot MCP Server
	botMcpMu           sync.RWMutex
	router             *gin.Engine
	httpServer         *http.Server
	port               string // 启动端口，如 ":18060"
	botID              string // 当前 handler 上下文的 botID（per-bot AppServer wrapper 使用）
}

// getOrCreateBotMCPServer 获取或创建指定 bot 的 MCP Server 实例
func (s *AppServer) getOrCreateBotMCPServer(botID string) *mcp.Server {
	s.botMcpMu.RLock()
	if srv, ok := s.botMcpServers[botID]; ok {
		s.botMcpMu.RUnlock()
		return srv
	}
	s.botMcpMu.RUnlock()

	s.botMcpMu.Lock()
	defer s.botMcpMu.Unlock()

	// double check
	if srv, ok := s.botMcpServers[botID]; ok {
		return srv
	}

	srv := InitMCPServerForBot(s, botID)
	s.botMcpServers[botID] = srv
	logrus.Infof("创建 bot %s 的 MCP Server 实例", botID)
	return srv
}

// NewAppServer 创建新的应用服务器实例
func NewAppServer(xiaohongshuService *XiaohongshuService) *AppServer {
	appServer := &AppServer{
		xiaohongshuService: xiaohongshuService,
		botMcpServers:      make(map[string]*mcp.Server),
	}

	// 初始化默认 MCP Server（兼容无 botID 的请求）
	appServer.defaultMcpServer = InitMCPServerForBot(appServer, "")

	return appServer
}

// Start 启动服务器
func (s *AppServer) Start(port string) error {
	s.port = port
	s.router = setupRoutes(s)

	s.httpServer = &http.Server{
		Addr:    port,
		Handler: s.router,
	}

	// 启动服务器的 goroutine
	go func() {
		logrus.Infof("启动 HTTP 服务器: %s", port)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("服务器启动失败: %v", err)
			os.Exit(1)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logrus.Infof("正在关闭服务器...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		logrus.Warnf("等待连接关闭超时，强制退出: %v", err)
	} else {
		logrus.Infof("服务器已优雅关闭")
	}

	return nil
}
