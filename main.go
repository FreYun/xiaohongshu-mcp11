package main

import (
	"flag"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
)

func main() {
	var (
		headless     bool
		binPath      string
		port         string
		profileDir   string // 单实例兼容：Chrome profile 目录
		profilesBase string // 多租户模式：profiles 根目录
	)
	flag.BoolVar(&headless, "headless", true, "是否无头模式")
	flag.StringVar(&binPath, "bin", "", "浏览器二进制文件路径")
	flag.StringVar(&port, "port", ":18060", "端口")
	flag.StringVar(&profileDir, "profile-dir", "", "Chrome user-data 目录（单实例兼容模式）")
	flag.StringVar(&profilesBase, "profiles-base", "/home/rooot/.xhs-profiles", "多租户模式的 profiles 根目录")
	flag.Parse()

	if len(binPath) == 0 {
		binPath = os.Getenv("ROD_BROWSER_BIN")
	}

	configs.InitHeadless(headless)
	configs.SetBinPath(binPath)
	configs.SetProfileDir(profileDir)
	configs.SetProfilesBase(profilesBase)

	// 初始化服务
	xiaohongshuService := NewXiaohongshuService()

	// 创建并启动应用服务器
	appServer := NewAppServer(xiaohongshuService)
	if err := appServer.Start(port); err != nil {
		logrus.Fatalf("failed to run server: %v", err)
	}
}
