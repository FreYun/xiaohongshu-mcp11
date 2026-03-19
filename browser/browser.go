package browser

import (
	"encoding/json"
	"net/url"
	"os"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/headless_browser"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

// Browser wraps headless_browser.Browser to provide a unified interface
// for both cookie-file mode and profile-dir mode.
type Browser struct {
	hb       *headless_browser.Browser // non-nil when using cookie-file mode
	rod      *rod.Browser              // non-nil when using profile-dir mode
	launcher *launcher.Launcher        // non-nil when using profile-dir mode
}

func (b *Browser) NewPage() *rod.Page {
	if b.hb != nil {
		return b.hb.NewPage()
	}
	return stealth.MustPage(b.rod)
}

func (b *Browser) Close() {
	if b.hb != nil {
		b.hb.Close()
		return
	}
	b.rod.MustClose()
	b.launcher.Cleanup()
}

type browserConfig struct {
	binPath    string
	profileDir string
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

	// Profile-dir mode: use rod/launcher directly with UserDataDir
	if cfg.profileDir != "" {
		return newBrowserWithProfile(headless, cfg)
	}

	// Default mode: use headless_browser with cookie-file loading
	opts := []headless_browser.Option{
		headless_browser.WithHeadless(headless),
	}
	if cfg.binPath != "" {
		opts = append(opts, headless_browser.WithChromeBinPath(cfg.binPath))
	}

	if proxy := os.Getenv("XHS_PROXY"); proxy != "" {
		opts = append(opts, headless_browser.WithProxy(proxy))
		logrus.Infof("Using proxy: %s", maskProxyCredentials(proxy))
	}

	// 加载 cookies
	cookiePath := cookies.GetCookiesFilePath()
	cookieLoader := cookies.NewLoadCookie(cookiePath)

	if data, err := cookieLoader.LoadCookies(); err == nil {
		opts = append(opts, headless_browser.WithCookies(string(data)))
		logrus.Debugf("loaded cookies from file successfully")
	} else {
		logrus.Warnf("failed to load cookies: %v", err)
	}

	return &Browser{hb: headless_browser.New(opts...)}
}

func newBrowserWithProfile(headless bool, cfg *browserConfig) *Browser {
	l := launcher.New().
		Headless(headless).
		UserDataDir(cfg.profileDir).
		Set("--no-sandbox")

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
	injectCookiesFromFile(b)

	return &Browser{rod: b, launcher: l}
}

// injectCookiesFromFile 将 cookies.json 中的 cookie 注入到浏览器实例中。
func injectCookiesFromFile(b *rod.Browser) {
	cookiePath := cookies.GetCookiesFilePath()
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

	params := proto.CookiesToParams(cks)
	setCookies := proto.NetworkSetCookies{Cookies: params}
	if err := setCookies.Call(b); err != nil {
		logrus.Warnf("profile mode: 注入 cookies 失败: %v", err)
		return
	}

	logrus.Infof("profile mode: 成功注入 %d 个 cookie", len(cks))
}
