package browser

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

const (
	closeTimeout = 10 * time.Second
	defaultUA    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// uaPool: realistic recent Chrome desktop UAs across Mac/Win.
// Each bot deterministically picks one based on botID hash, so the same bot
// always presents the same UA (changing UA per session is itself a bot signal).
var uaPool = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
}

// viewportPool: common real desktop resolutions. Same hash-stable selection.
var viewportPool = []struct{ W, H int }{
	{1920, 1080},
	{1680, 1050},
	{1536, 864},
	{1440, 900},
	{1366, 768},
}

// botSeed returns a stable hash of botID for deterministic pool selection.
func botSeed(botID string) uint32 {
	if botID == "" {
		return 0
	}
	h := fnv.New32a()
	h.Write([]byte(botID))
	return h.Sum32()
}

func uaForBot(botID string) string {
	if botID == "" {
		return defaultUA
	}
	return uaPool[botSeed(botID)%uint32(len(uaPool))]
}

func viewportForBot(botID string) (int, int) {
	if botID == "" {
		return 1920, 1080
	}
	v := viewportPool[botSeed(botID)%uint32(len(viewportPool))]
	return v.W, v.H
}

// applyStealthFlags adds anti-detection Chrome flags. The most important is
// --disable-blink-features=AutomationControlled, which removes the Blink-level
// automation signal that many bot detectors check (separate from
// navigator.webdriver, which the stealth JS already masks).
//
// We also explicitly Delete the --enable-automation flag that rod's launcher
// adds by default. That flag is the upstream source of navigator.webdriver=true
// and the "Chrome is being controlled by automated test software" infobar.
func applyStealthFlags(l *launcher.Launcher, botID string) *launcher.Launcher {
	w, h := viewportForBot(botID)
	return l.
		Delete("enable-automation").
		Set("disable-blink-features", "AutomationControlled").
		Set("user-agent", uaForBot(botID)).
		Set("window-size", fmt.Sprintf("%d,%d", w, h))
}

// Browser wraps rod.Browser + launcher to provide a unified interface
// for both cookie-file mode and profile-dir mode.
type Browser struct {
	rod      *rod.Browser
	launcher *launcher.Launcher
}

func (b *Browser) NewPage() *rod.Page {
	return stealth.MustPage(b.rod)
}

func (b *Browser) Close() {
	pid := b.launcher.PID()
	logrus.Infof("Browser.Close() called for Chrome PID %d", pid)

	// Step 1: try graceful DevTools close with timeout
	closeDone := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Warnf("Browser.Close() panicked: %v", r)
				closeDone <- fmt.Errorf("panic: %v", r)
			}
		}()
		closeDone <- b.rod.Close()
	}()

	var closeErr error
	select {
	case closeErr = <-closeDone:
		if closeErr != nil {
			logrus.Warnf("rod.Close() error for PID %d: %v", pid, closeErr)
		}
	case <-time.After(closeTimeout):
		logrus.Warnf("rod.Close() timed out for PID %d", pid)
	}

	// Step 2: wait briefly for Chrome process to actually exit
	exitDone := make(chan struct{})
	go func() {
		defer close(exitDone)
		b.launcher.Cleanup()
	}()

	select {
	case <-exitDone:
		logrus.Infof("Chrome PID %d exited cleanly", pid)
		return
	case <-time.After(5 * time.Second):
		logrus.Warnf("Chrome PID %d did not exit after 5s, force killing", pid)
		b.launcher.Kill()
	}
}

type browserConfig struct {
	binPath    string
	profileDir string
	cookiePath string // 指定 cookie 文件路径（多租户模式）
	botID      string // bot 标识，用于稳定选取 UA / viewport
}

type Option func(*browserConfig)

func WithBinPath(binPath string) Option {
	return func(c *browserConfig) {
		c.binPath = binPath
	}
}

// WithProfileDir sets the Chrome user data directory (profile directory).
// When set, cookies are loaded from the Chrome profile instead of from file.
func WithProfileDir(dir string) Option {
	return func(c *browserConfig) {
		c.profileDir = dir
	}
}

// WithCookiePath sets a specific cookie file path for this browser instance.
// When set, overrides the global cookies.GetCookiesFilePath().
func WithCookiePath(path string) Option {
	return func(c *browserConfig) {
		c.cookiePath = path
	}
}

// WithBotID sets the bot identifier so the launcher can pick a stable
// per-bot UA and viewport from the pool. Same botID always picks the same
// pair, which is the human-realistic behavior (varying UA per session is
// itself a bot signal).
func WithBotID(botID string) Option {
	return func(c *browserConfig) {
		c.botID = botID
	}
}

// maskProxyCredentials masks username and password in proxy URL for safe logging.
func maskProxyCredentials(proxyURL string) string {
	u, err := url.Parse(proxyURL)
	if err != nil || u.User == nil {
		return proxyURL
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.UserPassword("***", "***")
	} else {
		u.User = url.User("***")
	}
	return u.String()
}

func NewBrowser(headless bool, options ...Option) *Browser {
	cfg := &browserConfig{}
	for _, opt := range options {
		opt(cfg)
	}

	if cfg.profileDir != "" {
		return newBrowserWithProfile(headless, cfg)
	}

	return newBrowserWithCookies(headless, cfg)
}

// newBrowserWithCookies creates a browser with cookie-file loading (no persistent profile).
func newBrowserWithCookies(headless bool, cfg *browserConfig) *Browser {
	l := launcher.New().
		Headless(headless).
		Set("--no-sandbox")
	l = applyStealthFlags(l, cfg.botID)

	if cfg.binPath != "" {
		l = l.Bin(cfg.binPath)
	}

	if proxy := os.Getenv("XHS_PROXY"); proxy != "" {
		l = l.Proxy(proxy)
		logrus.Infof("Using proxy: %s", maskProxyCredentials(proxy))
	}

	u := l.MustLaunch()
	b := rod.New().ControlURL(u).MustConnect()

	// Inject cookies from file
	injectCookiesFromFile(b, cfg.cookiePath)

	return &Browser{rod: b, launcher: l}
}

func newBrowserWithProfile(headless bool, cfg *browserConfig) *Browser {
	// 清理残留的 SingletonLock，防止上次 Chrome 异常退出后锁文件未释放
	lockFile := filepath.Join(cfg.profileDir, "SingletonLock")
	if _, err := os.Stat(lockFile); err == nil {
		logrus.Warnf("profile mode [%s]: SingletonLock exists, removing (may indicate concurrent access or prior crash)", cfg.profileDir)
	}
	if err := os.Remove(lockFile); err == nil {
		logrus.Info("profile mode: 已清理残留的 SingletonLock")
	}

	l := launcher.New().
		Headless(headless).
		UserDataDir(cfg.profileDir).
		Set("--no-sandbox").
		Set("--disable-session-crashed-bubble").
		Set("--hide-crash-restore-bubble").
		Set("--no-first-run").
		Set("--disable-features", "SessionRestore")
	l = applyStealthFlags(l, cfg.botID)

	if cfg.binPath != "" {
		l = l.Bin(cfg.binPath)
	}

	if proxy := os.Getenv("XHS_PROXY"); proxy != "" {
		l = l.Proxy(proxy)
		logrus.Infof("Using proxy: %s", maskProxyCredentials(proxy))
	}

	u := l.MustLaunch()

	b := rod.New().
		ControlURL(u).
		MustConnect()

	// 注入 cookies.json 中的 cookie，使 profile 模式与 cookie 文件模式保持一致。
	// 这样即使是新建的 Chrome profile，也能携带已保存的 web_session 等认证 cookie。
	injectCookiesFromFile(b, cfg.cookiePath)

	return &Browser{rod: b, launcher: l}
}

// injectCookiesFromFile 通过 browser-level API 将 cookies.json 注入浏览器。
// cookiePathOverride 不为空时使用该路径代替全局路径。
func injectCookiesFromFile(b *rod.Browser, cookiePathOverride string) {
	cookiePath := cookiePathOverride
	if cookiePath == "" {
		cookiePath = cookies.GetCookiesFilePath()
	}
	cookieLoader := cookies.NewLoadCookie(cookiePath)

	data, err := cookieLoader.LoadCookies()
	if err != nil {
		logrus.Warnf("profile mode: 加载 cookies 文件失败（可能尚未登录）: %v", err)
		return
	}

	var cks []*proto.NetworkCookie
	if err := json.Unmarshal(data, &cks); err != nil {
		logrus.Warnf("profile mode: 解析 cookies 失败: %v", err)
		return
	}

	// 用 browser.SetCookies 替代 Network.setCookies，可在 browser target 上调用
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
		logrus.Warnf("profile mode: 注入 cookies 失败: %v", err)
		return
	}

	logrus.Infof("profile mode: 成功注入 %d 个 cookie", len(cks))
}
