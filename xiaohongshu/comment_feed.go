package xiaohongshu

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sirupsen/logrus"
)

// CommentFeedAction 表示 Feed 评论动作
type CommentFeedAction struct {
	page *rod.Page
}

// NewCommentFeedAction 创建 Feed 评论动作
func NewCommentFeedAction(page *rod.Page) *CommentFeedAction {
	return &CommentFeedAction{page: page}
}

// PostComment 发表评论到 Feed
func (f *CommentFeedAction) PostComment(ctx context.Context, feedID, xsecToken, content string) error {
	// 不使用 Context(ctx)，避免继承外部 context 的超时
	page := f.page.Timeout(60 * time.Second)

	url := makeFeedDetailURL(feedID, xsecToken)
	logrus.Infof("打开 feed 详情页: %s", url)

	// 导航到详情页
	page.MustNavigate(url)
	page.MustWaitDOMStable()
	time.Sleep(1 * time.Second)

	// 检测页面是否可访问
	if err := checkPageAccessible(page); err != nil {
		return err
	}

	elem, err := page.Element("div.input-box div.content-edit span")
	if err != nil {
		logrus.Warnf("Failed to find comment input box: %v", err)
		return fmt.Errorf("未找到评论输入框，该帖子可能不支持评论或网页端不可访问: %w", err)
	}

	if err := elem.Click(proto.InputMouseButtonLeft, 1); err != nil {
		logrus.Warnf("Failed to click comment input box: %v", err)
		return fmt.Errorf("无法点击评论输入框: %w", err)
	}

	elem2, err := page.Element("div.input-box div.content-edit p.content-input")
	if err != nil {
		logrus.Warnf("Failed to find comment input field: %v", err)
		return fmt.Errorf("未找到评论输入区域: %w", err)
	}

	if err := elem2.Input(content); err != nil {
		logrus.Warnf("Failed to input comment content: %v", err)
		return fmt.Errorf("无法输入评论内容: %w", err)
	}

	time.Sleep(1 * time.Second)

	submitButton, err := page.Element("div.bottom button.submit")
	if err != nil {
		logrus.Warnf("Failed to find submit button: %v", err)
		return fmt.Errorf("未找到提交按钮: %w", err)
	}

	if err := submitButton.Click(proto.InputMouseButtonLeft, 1); err != nil {
		logrus.Warnf("Failed to click submit button: %v", err)
		return fmt.Errorf("无法点击提交按钮: %w", err)
	}

	time.Sleep(1 * time.Second)

	logrus.Infof("Comment posted successfully to feed: %s", feedID)
	return nil
}

// ReplyToComment 回复指定评论
func (f *CommentFeedAction) ReplyToComment(ctx context.Context, feedID, xsecToken, commentID, userID, content string) error {
	// 增加超时时间，因为需要滚动查找评论
	// 注意：不使用 Context(ctx)，避免继承外部 context 的超时
	page := f.page.Timeout(5 * time.Minute)
	url := makeFeedDetailURL(feedID, xsecToken)
	logrus.Infof("打开 feed 详情页进行回复: %s", url)

	// 导航到详情页
	page.MustNavigate(url)
	page.MustWaitDOMStable()
	time.Sleep(1 * time.Second)

	// 检测页面是否可访问
	if err := checkPageAccessible(page); err != nil {
		return err
	}

	// 等待评论容器加载
	time.Sleep(2 * time.Second)

	// 使用 Go 实现的查找逻辑
	commentEl, err := findCommentElement(page, commentID, userID)
	if err != nil {
		return fmt.Errorf("无法找到评论: %w", err)
	}

	// 滚动到评论位置
	logrus.Info("滚动到评论位置...")
	commentEl.MustScrollIntoView()
	time.Sleep(1 * time.Second)

	logrus.Info("准备点击回复按钮")

	// 查找并点击回复按钮
	replyBtn, err := commentEl.Element(".right .interactions .reply")
	if err != nil {
		return fmt.Errorf("无法找到回复按钮: %w", err)
	}

	if err := replyBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("点击回复按钮失败: %w", err)
	}

	time.Sleep(1 * time.Second)

	// 查找回复输入框
	inputEl, err := page.Element("div.input-box div.content-edit p.content-input")
	if err != nil {
		return fmt.Errorf("无法找到回复输入框: %w", err)
	}

	// 输入内容
	if err := inputEl.Input(content); err != nil {
		return fmt.Errorf("输入回复内容失败: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	// 查找并点击提交按钮
	submitBtn, err := page.Element("div.bottom button.submit")
	if err != nil {
		return fmt.Errorf("无法找到提交按钮: %w", err)
	}

	if err := submitBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("点击提交按钮失败: %w", err)
	}

	time.Sleep(2 * time.Second)
	logrus.Infof("回复评论成功")
	return nil
}

// findCommentElement 查找指定评论元素（参考 feed_detail.go 的滚动逻辑）
func findCommentElement(page *rod.Page, commentID, userID string) (*rod.Element, error) {
	logrus.Infof("开始查找评论 - commentID: %s, userID: %s", commentID, userID)

	const maxAttempts = 100
	const scrollInterval = 800 * time.Millisecond

	// 先滚动到评论区
	scrollToCommentsArea(page)
	time.Sleep(1 * time.Second)

	var lastCommentCount = 0
	stagnantChecks := 0

	logrus.Infof("开始循环查找，最大尝试次数: %d", maxAttempts)

	// DEBUG: 打印前几个评论元素的实际属性，帮助定位选择器
	debugResult, debugErr := page.Eval(`() => {
		const comments = document.querySelectorAll('.parent-comment, .comment-item, [id*="comment"]');
		const info = [];
		comments.forEach((el, i) => {
			if (i < 6) {
				info.push({
					tag: el.tagName,
					id: el.id || '',
					classes: String(el.className || '').substring(0, 80),
					dataAttrs: Object.keys(el.dataset || {}).map(k => k + '=' + String(el.dataset[k]).substring(0, 40)),
					outerStart: el.outerHTML.substring(0, 200)
				});
			}
		});
		return JSON.stringify(info);
	}`)
	if debugErr == nil {
		logrus.Infof("DEBUG 评论元素结构: %s", debugResult.Value.Str())
	} else {
		logrus.Warnf("DEBUG 评论元素检查失败: %v", debugErr)
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		logrus.Infof("=== 查找尝试 %d/%d ===", attempt+1, maxAttempts)

		// === 1. 先尝试查找目标评论（优先于任何退出判断）===
		if commentID != "" {
			selector := fmt.Sprintf("#comment-%s", commentID)
			el, err := page.Timeout(2 * time.Second).Element(selector)
			if err == nil && el != nil {
				logrus.Infof("✓ 通过 commentID 找到评论: %s (尝试 %d 次)", commentID, attempt+1)
				return el, nil
			}
		}

		// 通过 userID 查找
		if userID != "" && commentID == "" {
			elements, err := page.Timeout(2 * time.Second).Elements(".comment-item")
			if err == nil {
				for _, el := range elements {
					userEl, err := el.Timeout(500 * time.Millisecond).Element(fmt.Sprintf(`[data-user-id="%s"]`, userID))
					if err == nil && userEl != nil {
						logrus.Infof("✓ 通过 userID 找到评论 (尝试 %d 次)", attempt+1)
						return el, nil
					}
				}
			}
		}

		// === 2. 没找到，检查是否到底 ===
		if attempt > 3 && checkEndContainer(page) {
			logrus.Info("已到达评论底部且未找到目标评论")
			break
		}

		// === 3. 获取当前评论数量 ===
		currentCount := getCommentCount(page)
		if currentCount != lastCommentCount {
			lastCommentCount = currentCount
			stagnantChecks = 0
		} else {
			stagnantChecks++
		}

		// === 4. 停滞检测 ===
		if stagnantChecks >= 15 {
			logrus.Infof("评论数量停滞超过15次 (当前 %d 条)，放弃查找", currentCount)
			break
		}

		// === 5. 滚动加载更多评论 ===
		_, err := page.Eval(`() => {
			const container = document.querySelector('.note-scroller') || document.querySelector('.interaction-container');
			if (container) { container.scrollBy(0, 500); }
			else { window.scrollBy(0, window.innerHeight * 0.8); }
			return true;
		}`)
		if err != nil {
			logrus.Warnf("滚动失败: %v", err)
		}
		time.Sleep(800 * time.Millisecond)
	}

	return nil, fmt.Errorf("未找到评论 (commentID: %s, userID: %s), 尝试次数: %d", commentID, userID, maxAttempts)
}
