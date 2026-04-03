package xiaohongshu

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-rod/rod"
)

// ErrNotLoggedIn 表示操作因未登录而失败，调用方应终止操作而非继续等待。
var ErrNotLoggedIn = fmt.Errorf("未登录")

// checkCreatorPageLogin 检查当前页面是否被重定向到创作者平台登录页。
// 在导航到任何 creator.xiaohongshu.com 页面后调用。
// 如果检测到登录页，返回 ErrNotLoggedIn；否则返回 nil。
func checkCreatorPageLogin(page *rod.Page) error {
	info, err := page.Info()
	if err != nil {
		return nil // 无法获取页面信息，不阻塞，让后续逻辑处理
	}
	url := info.URL
	if strings.Contains(url, "/login") || strings.Contains(url, "redirectReason=401") {
		return fmt.Errorf("%w: 创作者平台登录已失效（被重定向到 %s）", ErrNotLoggedIn, url)
	}
	return nil
}

// checkMainSiteLogin 检查当前页面是否被重定向到主站登录页。
func checkMainSiteLogin(page *rod.Page) error {
	info, err := page.Info()
	if err != nil {
		return nil
	}
	url := info.URL
	// captcha 不算未登录（cookie 可能有效，只是被反爬拦截）
	if strings.Contains(url, "/captcha") {
		return nil
	}
	if strings.Contains(url, "/login") {
		return fmt.Errorf("%w: 主站登录已失效（被重定向到 %s）", ErrNotLoggedIn, url)
	}
	return nil
}

type NavigateAction struct {
	page *rod.Page
}

func NewNavigate(page *rod.Page) *NavigateAction {
	return &NavigateAction{page: page}
}

func (n *NavigateAction) ToExplorePage(ctx context.Context) error {
	page := n.page.Context(ctx)

	page.MustNavigate("https://www.xiaohongshu.com/explore").
		MustWaitLoad().
		MustElement(`div#app`)

	return nil
}

func (n *NavigateAction) ToProfilePage(ctx context.Context) error {
	page := n.page.Context(ctx)

	// First navigate to explore page
	if err := n.ToExplorePage(ctx); err != nil {
		return err
	}

	page.MustWaitStable()

	// Find and click the "我" channel link in sidebar
	profileLink := page.MustElement(`div.main-container li.user.side-bar-component a.link-wrapper span.channel`)
	profileLink.MustClick()

	// Wait for navigation to complete
	page.MustWaitLoad()

	return nil
}
