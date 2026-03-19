package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

func main() {
	var (
		headless   bool
		binPath    string // 浏览器二进制文件路径
		port       string
		profileDir string // Chrome profile 目录（user-data-dir）
	)
	flag.BoolVar(&headless, "headless", true, "是否无头模式")
	flag.StringVar(&binPath, "bin", "", "浏览器二进制文件路径")
	flag.StringVar(&port, "port", ":18060", "端口")
	flag.StringVar(&profileDir, "profile-dir", "", "Chrome user-data 目录，如 /home/rooot/.openclaw/browser/bot1/user-data")
	flag.Parse()

	if len(binPath) == 0 {
		binPath = os.Getenv("ROD_BROWSER_BIN")
	}

	configs.InitHeadless(headless)
	configs.SetBinPath(binPath)
	configs.SetProfileDir(profileDir)

	// 根据端口号设置独立的 cookie 文件，避免多 bot 共享同一 session
	portNum, _ := strconv.Atoi(strings.TrimPrefix(port, ":"))
	if portNum >= 18061 && portNum <= 18070 {
		botID := fmt.Sprintf("bot%d", portNum-18060)
		cookiePath := fmt.Sprintf("cookies-%s.json", botID)
		cookies.SetCookiesFilePath(cookiePath)
		logrus.Infof("cookie 文件: %s", cookiePath)
	}

	// 初始化服务
	xiaohongshuService := NewXiaohongshuService()

	// 创建并启动应用服务器
	appServer := NewAppServer(xiaohongshuService)
	if err := appServer.Start(port); err != nil {
		logrus.Fatalf("failed to run server: %v", err)
	}
}
