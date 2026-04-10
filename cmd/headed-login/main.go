package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

// 与 MCP browser.go 保持一致的 UA
const defaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

func main() {
	botID := flag.String("bot", "", "bot ID, e.g. bot4")
	profilesBase := flag.String("profiles-base", "/home/rooot/.xhs-profiles", "profiles base dir")
	flag.Parse()

	if *botID == "" {
		fmt.Println("Usage: go run ./cmd/headed-login -bot=bot4")
		os.Exit(1)
	}

	cookies.SetProfilesBase(*profilesBase)
	cookiePath := cookies.GetCookiesFilePathForBot(*botID)
	fmt.Printf("Bot: %s\nCookie file: %s\n", *botID, cookiePath)

	// 检查是否有 profile 目录（与 MCP configs.GetProfileDirForBot 逻辑一致）
	profileDir := ""
	p1 := filepath.Join(*profilesBase, *botID)
	if info, err := os.Stat(p1); err == nil && info.IsDir() {
		profileDir = p1
	}

	if profileDir != "" {
		fmt.Printf("Profile 模式: %s\n", profileDir)
		// 清理残留 SingletonLock
		lockFile := filepath.Join(profileDir, "SingletonLock")
		if err := os.Remove(lockFile); err == nil {
			fmt.Println("已清理残留 SingletonLock")
		}
	} else {
		fmt.Println("Cookie 文件模式（无 profile 目录）")
	}

	// 构建 launcher，与 MCP 保持一致
	l := launcher.New().
		Headless(false).
		Set("--no-sandbox").
		Set("--window-size", "1920,1080").
		Set("--start-maximized").
		Set("user-agent", defaultUA)

	if profileDir != "" {
		l = l.UserDataDir(profileDir)
	}

	u, err := l.Launch()
	if err != nil {
		fmt.Printf("⚠️  Headed 模式启动失败: %v，降级为 headless\n", err)
		l = launcher.New().
			Headless(true).
			Set("--no-sandbox").
			Set("user-agent", defaultUA)
		if profileDir != "" {
			l = l.UserDataDir(profileDir)
		}
		u = l.MustLaunch()
	} else {
		fmt.Println("✅ Headed 模式启动成功")
	}

	b := rod.New().ControlURL(u).MustConnect()
	defer b.MustClose()

	page := b.MustPage()

	// 注入已有 cookie（profile 模式下也注入，与 MCP 行为一致）
	injected := injectCookiesFromFile(b, cookiePath)
	if injected > 0 {
		fmt.Printf("已注入 %d 个已有 cookie\n", injected)
	} else {
		fmt.Println("无已有 cookie，需要扫码登录")
	}

	// 导航到 explore 页面
	fmt.Println("导航到小红书...")
	page.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
	time.Sleep(3 * time.Second)

	// 检查当前是否已登录
	if checkLoginCookies(b) {
		fmt.Println("✅ 当前 cookie 有效，已登录。")
		saveCookies(b, cookiePath)
		fmt.Println("浏览器保持打开，Ctrl+C 退出。")
		sigWait := make(chan os.Signal, 1)
		signal.Notify(sigWait, syscall.SIGINT, syscall.SIGTERM)
		<-sigWait
		saveCookies(b, cookiePath)
		fmt.Println("\n退出，cookie 已保存。")
		return
	}
	fmt.Println("当前 cookie 无效或未登录，需要扫码。")

	// 记录初始 web_session（用于检测变化）
	initialWebSession := getWebSession(b)

	// 统一的 signal channel，避免多个 handler 竞争
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 等待扫码
	fmt.Println("\n📱 请用小红书 App 扫码登录，等待中...")
	fmt.Println("随时可以 Ctrl+C 退出，会自动检查登录状态并保存 cookie。")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	tick := 0
	smsHandled := false
	loggedIn := false

	for !loggedIn {
		select {
		case <-sigCh:
			fmt.Println("\n\n收到退出信号，检查登录状态...")
			saveCookies(b, cookiePath)
			if checkLoginStatus(b, initialWebSession) {
				fmt.Println("✅ 登录成功，cookie 已保存。")
			} else {
				fmt.Println("❌ 登录未成功，cookie 状态不可用。")
			}
			return
		case <-ctx.Done():
			fmt.Println("\n⏰ 超时，Ctrl+C 退出。")
			<-sigCh
			saveCookies(b, cookiePath)
			return
		case <-ticker.C:
			tick++

			// 检测 SMS 验证弹窗
			if !smsHandled {
				if hasSMSDialog(page) {
					smsHandled = true
					fmt.Println("\n🔐 检测到短信验证码弹窗！请在浏览器中完成验证。")
					fmt.Println("完成后 Ctrl+C 退出，会自动检查登录状态。")
				}
			}

			// 检查 cookie 变化（扫码成功无需 SMS 的情况）
			newSession := getWebSession(b)
			if newSession != "" && newSession != initialWebSession {
				fmt.Println("\n✅ 检测到新 web_session，保存 cookies...")
				saveCookies(b, cookiePath)
				fmt.Println("🎉 登录成功！cookie 已保存。")
				loggedIn = true
			}

			// 心跳
			if tick%5 == 0 {
				fmt.Printf("[%ds] 等待中...\n", tick*2)
			}
		}
	}

	// 登录成功，保持浏览器打开
	fmt.Println("浏览器保持打开，Ctrl+C 退出。")
	<-sigCh
	saveCookies(b, cookiePath)
	fmt.Println("\n退出，cookie 已保存。")
}

// injectCookiesFromFile 从 cookie 文件加载并注入到浏览器，返回注入数量
func injectCookiesFromFile(b *rod.Browser, cookiePath string) int {
	cookieLoader := cookies.NewLoadCookie(cookiePath)
	data, err := cookieLoader.LoadCookies()
	if err != nil {
		return 0
	}

	var cks []*proto.NetworkCookie
	if err := json.Unmarshal(data, &cks); err != nil {
		return 0
	}

	var rodCookies []*proto.NetworkCookieParam
	for _, ck := range cks {
		rodCookies = append(rodCookies, &proto.NetworkCookieParam{
			Name:     ck.Name,
			Value:    ck.Value,
			Domain:   ck.Domain,
			Path:     ck.Path,
			Secure:   ck.Secure,
			HTTPOnly: ck.HTTPOnly,
			SameSite: ck.SameSite,
			Expires:  ck.Expires,
		})
	}
	if err := b.SetCookies(rodCookies); err != nil {
		return 0
	}
	return len(cks)
}

// checkLoginCookies 检查浏览器中是否有有效的登录 cookie
func checkLoginCookies(b *rod.Browser) bool {
	cks, err := b.GetCookies()
	if err != nil {
		return false
	}
	hasSession := false
	hasToken := false
	for _, c := range cks {
		if c.Name == "web_session" && c.Value != "" {
			hasSession = true
		}
		if c.Name == "id_token" && c.Value != "" {
			hasToken = true
		}
	}
	return hasSession && hasToken
}

// hasSMSDialog 检测页面上是否有 SMS 验证码弹窗
func hasSMSDialog(page *rod.Page) bool {
	for _, selector := range []string{
		`text=短信验证码验证`,
		`text=SMS Verification`,
	} {
		if exists, _, _ := page.Has(selector); exists {
			return true
		}
	}
	return false
}

func getWebSession(b *rod.Browser) string {
	cks, err := b.GetCookies()
	if err != nil {
		return ""
	}
	for _, c := range cks {
		if c.Name == "web_session" {
			return c.Value
		}
	}
	return ""
}

func saveCookies(b *rod.Browser, cookiePath string) {
	cks, err := b.GetCookies()
	if err != nil {
		fmt.Printf("获取 cookies 失败: %v\n", err)
		return
	}

	data, err := json.Marshal(cks)
	if err != nil {
		fmt.Printf("序列化 cookies 失败: %v\n", err)
		return
	}

	loader := cookies.NewLoadCookie(cookiePath)
	if err := loader.SaveCookies(data); err != nil {
		fmt.Printf("保存 cookies 失败: %v\n", err)
	} else {
		fmt.Printf("已保存 %d 个 cookie 到 %s\n", len(cks), cookiePath)
	}
}

func checkLoginStatus(b *rod.Browser, initialWebSession string) bool {
	ws := getWebSession(b)
	if ws == "" || ws == initialWebSession {
		return false
	}
	return checkLoginCookies(b)
}
