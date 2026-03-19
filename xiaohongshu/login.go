package xiaohongshu

import (
	"context"
	"time"

	"github.com/go-rod/rod"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type LoginAction struct {
	page *rod.Page
}

func NewLogin(page *rod.Page) *LoginAction {
	return &LoginAction{page: page}
}

func (a *LoginAction) CheckLoginStatus(_ context.Context) (bool, error) {
	// 直接检查浏览器 cookie，避免依赖易变的 CSS selector
	cks, err := a.page.Browser().GetCookies()
	if err != nil {
		return false, errors.Wrap(err, "get cookies failed")
	}
	for _, c := range cks {
		if c.Name == "web_session" && c.Value != "" {
			return true, nil
		}
	}
	return false, nil
}

func (a *LoginAction) Login(ctx context.Context) error {
	pp := a.page.Context(ctx)

	// 导航到小红书首页，这会触发二维码弹窗
	pp.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()

	// 等待一小段时间让页面完全加载
	time.Sleep(2 * time.Second)

	// 检查是否已经登录
	if exists, _, _ := pp.Has(".main-container .user .link-wrapper .channel"); exists {
		// 已经登录，直接返回
		return nil
	}

	// 等待扫码成功提示或者登录完成
	// 这里我们等待登录成功的元素出现，这样更简单可靠
	pp.MustElement(".main-container .user .link-wrapper .channel")

	return nil
}

func (a *LoginAction) FetchQrcodeImage(ctx context.Context) (string, bool, error) {
	pp := a.page.Context(ctx)

	// 导航到小红书首页，这会触发二维码弹窗
	pp.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()

	// 等待一小段时间让页面完全加载
	time.Sleep(2 * time.Second)

	// 检查是否已经登录
	if exists, _, _ := pp.Has(".main-container .user .link-wrapper .channel"); exists {
		return "", true, nil
	}

	// 获取二维码图片
	src, err := pp.MustElement(".login-container .qrcode-img").Attribute("src")
	if err != nil {
		return "", false, errors.Wrap(err, "get qrcode src failed")
	}
	if src == nil || len(*src) == 0 {
		return "", false, errors.New("qrcode src is empty")
	}

	return *src, false, nil
}

func (a *LoginAction) WaitForLogin(ctx context.Context) bool {
	// 记录初始 web_session，只有值变化时才认为新登录成功，防止误判旧 session
	initialWebSession := ""
	if cks, err := a.page.Browser().GetCookies(); err == nil {
		for _, c := range cks {
			if c.Name == "web_session" {
				initialWebSession = c.Value
				break
			}
		}
	}
	logrus.Infof("WaitForLogin start: initialWebSession empty=%v", initialWebSession == "")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			// 用 Has 非阻塞检测，避免 Element() 自带超时卡住轮询
			pp := a.page.Context(ctx)
			if exists, _, _ := pp.Has(".main-container .user .link-wrapper .channel"); exists {
				logrus.Info("WaitForLogin: detected login via CSS selector")
				return true
			}
			// 备选：检测 web_session cookie 变化（新登录后浏览器会收到新 cookie）
			cks, err := a.page.Browser().GetCookies()
			if err == nil {
				for _, c := range cks {
					if c.Name == "web_session" && c.Value != "" && c.Value != initialWebSession {
						logrus.Infof("WaitForLogin: detected new web_session cookie")
						return true
					}
				}
			}
		}
	}
}
