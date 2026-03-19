package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sirupsen/logrus"
)

const (
	urlNotificationMentions = "https://www.xiaohongshu.com/notification?channelTabId=mentions"
)

// NotificationComment 通知页中的单条评论
type NotificationComment struct {
	Index       int    `json:"index"`
	Username    string `json:"username"`
	UserID      string `json:"user_id,omitempty"`
	Action      string `json:"action"`
	Content     string `json:"content"`
	Time        string `json:"time"`
	NoteTitle   string `json:"note_title,omitempty"`
	NoteSnippet string `json:"note_snippet,omitempty"`
}

// NotificationListResponse 通知列表响应
type NotificationListResponse struct {
	Comments []NotificationComment `json:"comments"`
	Count    int                   `json:"count"`
}

// NotificationAction 通知页操作
type NotificationAction struct {
	page *rod.Page
}

// NewNotificationAction 创建通知页操作实例
func NewNotificationAction(page *rod.Page) *NotificationAction {
	return &NotificationAction{page: page}
}

// GetNotificationComments 获取通知页「评论和@」列表
func (n *NotificationAction) GetNotificationComments(ctx context.Context) (*NotificationListResponse, error) {
	page := n.page.Timeout(60 * time.Second)

	logrus.Info("导航到通知页面 - 评论和@")
	if err := page.Navigate(urlNotificationMentions); err != nil {
		return nil, fmt.Errorf("导航到通知页面失败: %w", err)
	}
	page.MustWaitLoad()
	humanSleep(2 * time.Second)

	if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("等待 DOM 稳定出现问题: %v，继续尝试", err)
	}

	// 确保在「评论和@」tab
	if err := ensureMentionsTab(page); err != nil {
		logrus.Warnf("切换评论和@tab失败: %v，继续尝试读取", err)
	}
	humanSleep(1 * time.Second)

	// 提取评论列表
	comments, err := extractNotificationComments(page)
	if err != nil {
		return nil, fmt.Errorf("提取评论列表失败: %w", err)
	}

	return &NotificationListResponse{
		Comments: comments,
		Count:    len(comments),
	}, nil
}

// ReplyToNotificationComment 在通知页回复指定评论
func (n *NotificationAction) ReplyToNotificationComment(ctx context.Context, commentIndex int, replyContent string) error {
	page := n.page.Timeout(60 * time.Second)

	logrus.Infof("导航到通知页面，准备回复第 %d 条评论", commentIndex)
	if err := page.Navigate(urlNotificationMentions); err != nil {
		return fmt.Errorf("导航到通知页面失败: %w", err)
	}
	page.MustWaitLoad()
	humanSleep(2 * time.Second)

	if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("等待 DOM 稳定出现问题: %v，继续尝试", err)
	}

	// 确保在「评论和@」tab
	if err := ensureMentionsTab(page); err != nil {
		logrus.Warnf("切换评论和@tab失败: %v，继续尝试", err)
	}
	humanSleep(1 * time.Second)

	// 点击对应评论的「回复」按钮
	if err := clickReplyButton(page, commentIndex); err != nil {
		return fmt.Errorf("点击回复按钮失败: %w", err)
	}
	humanSleep(1 * time.Second)

	// 找到回复输入框并输入内容
	if err := inputReplyContent(page, replyContent); err != nil {
		return fmt.Errorf("输入回复内容失败: %w", err)
	}
	humanSleep(500 * time.Millisecond)

	// 点击发送
	if err := clickSendButton(page); err != nil {
		return fmt.Errorf("点击发送按钮失败: %w", err)
	}

	humanSleep(2 * time.Second)
	logrus.Infof("通知页回复第 %d 条评论成功", commentIndex)
	return nil
}

// ensureMentionsTab 确保在「评论和@」tab 下
func ensureMentionsTab(page *rod.Page) error {
	// 使用 go-rod 查找 tab 元素并点击
	tabs, err := page.Elements(".tab-item, .channel-tab-item, [class*=tab]")
	if err != nil {
		logrus.Warnf("查找 tab 元素失败: %v", err)
		return nil
	}

	for _, tab := range tabs {
		text, err := tab.Text()
		if err != nil {
			continue
		}
		if strings.Contains(text, "评论和@") {
			// 检查是否已激活
			cls, _ := tab.Attribute("class")
			if cls != nil && strings.Contains(*cls, "active") {
				logrus.Info("已在「评论和@」tab")
				return nil
			}
			if err := tab.Click(proto.InputMouseButtonLeft, 1); err != nil {
				logrus.Warnf("go-rod 点击 tab 失败: %v，尝试 JS 方式", err)
			} else {
				logrus.Info("已切换到「评论和@」tab")
				humanSleep(1 * time.Second)
				return nil
			}
		}
	}

	// 兜底：用 JS 点击包含「评论和@」文本的元素
	logrus.Info("尝试 JS 方式切换到「评论和@」tab")
	_, err = page.Eval(`() => {
		const allElements = document.querySelectorAll('*');
		for (let el of allElements) {
			if (el.children.length === 0 && el.textContent.trim() === '评论和@') {
				el.click();
				return true;
			}
		}
		return false;
	}`)
	if err != nil {
		return fmt.Errorf("JS 切换 tab 失败: %w", err)
	}

	humanSleep(1 * time.Second)
	return nil
}

// extractNotificationComments 从通知页提取评论列表
// 核心策略：用 JS 找到每个包含用户链接的最小粒度通知条目，过滤掉 tab 栏等无关元素
func extractNotificationComments(page *rod.Page) ([]NotificationComment, error) {
	return extractCommentsViaJS(page)
}

// extractCommentsViaJS 通过 JS 提取评论信息
// 策略：找到所有包含 a[href*="/user/profile/"] 的最小独立条目，每个条目对应一条通知
func extractCommentsViaJS(page *rod.Page) ([]NotificationComment, error) {
	result, err := page.Eval(`() => {
		const comments = [];

		// 跳过 tab 栏的关键词（完全匹配去空格后的文本）
		const tabBarTexts = ['评论和@赞和收藏新增关注', '评论和@', '赞和收藏', '新增关注'];

		// 策略 1: 找所有包含用户链接的通知条目
		// 小红书通知页的每条通知都包含一个用户头像链接
		const userLinks = document.querySelectorAll('a[href*="/user/profile/"]');
		if (userLinks.length === 0) {
			return JSON.stringify([]);
		}

		// 从每个用户链接向上找到最近的"通知条目"容器
		// 通知条目通常是 userLink 的某个祖先，且包含回复按钮和时间信息
		const seenItems = new Set();

		for (const link of userLinks) {
			// 向上查找包含"回复"按钮或时间信息的最小容器
			let candidate = link.parentElement;
			let bestCandidate = null;

			for (let depth = 0; depth < 8 && candidate; depth++) {
				const text = candidate.innerText || '';
				const stripped = text.replace(/\s+/g, '');

				// 跳过 tab 栏
				if (tabBarTexts.some(t => stripped === t)) {
					candidate = candidate.parentElement;
					continue;
				}

				// 如果这个元素包含时间信息（"天前"、"小时前"、"分钟前"、"昨天"等）
				// 并且包含操作信息（"评论了你的笔记"、"回复了你的评论"、"@了你"）
				// 那它很可能是一个完整的通知条目
				const hasTimeInfo = /(\d+天前|\d+小时前|\d+分钟前|昨天|前天|刚刚|\d{1,2}:\d{2})/.test(text);
				const hasAction = /(评论了你的笔记|回复了你的评论|@了你|在评论中@了你|提到了你)/.test(text);

				if (hasTimeInfo && hasAction) {
					bestCandidate = candidate;
					break;
				}

				// 如果当前元素的兄弟元素也包含用户链接，说明当前层级太高（包含了多条通知）
				if (candidate.parentElement) {
					const siblings = candidate.parentElement.children;
					let siblingsWithUserLinks = 0;
					for (const sib of siblings) {
						if (sib !== candidate && sib.querySelector && sib.querySelector('a[href*="/user/profile/"]')) {
							siblingsWithUserLinks++;
						}
					}
					if (siblingsWithUserLinks > 0 && bestCandidate === null) {
						bestCandidate = candidate;
						break;
					}
				}

				candidate = candidate.parentElement;
			}

			if (!bestCandidate) continue;

			// 用元素引用去重
			const itemKey = bestCandidate.innerHTML.substring(0, 100);
			if (seenItems.has(itemKey)) continue;
			seenItems.add(itemKey);

			const text = bestCandidate.innerText || '';
			const stripped = text.replace(/\s+/g, '');

			// 再次过滤 tab 栏
			if (tabBarTexts.some(t => stripped === t)) continue;
			if (text.trim().length < 10) continue;

			// 提取用户名和 ID
			const firstLink = bestCandidate.querySelector('a[href*="/user/profile/"]');
			let username = '';
			let userId = '';
			if (firstLink) {
				username = firstLink.innerText.trim();
				const m = firstLink.href.match(/\/user\/profile\/([^?/]+)/);
				if (m) userId = m[1];
			}

			// 判断操作类型
			let action = '';
			if (text.includes('评论了你的笔记')) action = '评论了你的笔记';
			else if (text.includes('回复了你的评论')) action = '回复了你的评论';
			else if (text.includes('@了你') || text.includes('在评论中@了你') || text.includes('提到了你')) action = '在评论中@了你';

			// 提取时间
			let time = '';
			const timeMatch = text.match(/(\d+天前|\d+小时前|\d+分钟前|昨天 \d{1,2}:\d{2}|前天 \d{1,2}:\d{2}|刚刚|\d{1,2}:\d{2})/);
			if (timeMatch) time = timeMatch[1];

			// 提取评论内容（去掉用户名和操作描述后的部分）
			let content = text.trim();
			if (content.length > 300) content = content.substring(0, 300) + '...';

			comments.push({
				index: comments.length,
				username: username,
				user_id: userId,
				action: action,
				content: content,
				time: time,
				note_title: '',
				note_snippet: ''
			});
		}

		return JSON.stringify(comments);
	}`)
	if err != nil {
		return nil, fmt.Errorf("JS 提取评论失败: %w", err)
	}

	raw := result.Value.Str()
	if raw == "" || raw == "[]" {
		logrus.Warn("JS 未提取到任何通知评论")
		return nil, nil
	}

	logrus.Infof("JS 提取到评论数据: %d 字符", len(raw))

	var comments []NotificationComment
	if err := json.Unmarshal([]byte(raw), &comments); err != nil {
		return nil, fmt.Errorf("解析评论 JSON 失败: %w", err)
	}

	logrus.Infof("成功解析 %d 条通知评论", len(comments))
	return comments, nil
}

// clickReplyButton 点击通知页第 n 条评论的「回复」按钮
// 使用与 extractCommentsViaJS 一致的逻辑定位通知条目，然后在条目内找回复按钮
func clickReplyButton(page *rod.Page, index int) error {
	logrus.Infof("尝试点击第 %d 条评论的回复按钮", index)

	result, err := page.Eval(fmt.Sprintf(`() => {
		const tabBarTexts = ['评论和@赞和收藏新增关注', '评论和@', '赞和收藏', '新增关注'];

		// 与 extractCommentsViaJS 一致的定位逻辑
		const userLinks = document.querySelectorAll('a[href*="/user/profile/"]');
		if (userLinks.length === 0) {
			return JSON.stringify({ clicked: false, error: "no user links found on page" });
		}

		const seenItems = new Set();
		const notificationItems = [];

		for (const link of userLinks) {
			let candidate = link.parentElement;
			let bestCandidate = null;

			for (let depth = 0; depth < 8 && candidate; depth++) {
				const text = candidate.innerText || '';
				const stripped = text.replace(/\s+/g, '');
				if (tabBarTexts.some(t => stripped === t)) { candidate = candidate.parentElement; continue; }

				const hasTimeInfo = /(\d+天前|\d+小时前|\d+分钟前|昨天|前天|刚刚|\d{1,2}:\d{2})/.test(text);
				const hasAction = /(评论了你的笔记|回复了你的评论|@了你|在评论中@了你|提到了你)/.test(text);

				if (hasTimeInfo && hasAction) { bestCandidate = candidate; break; }

				if (candidate.parentElement) {
					const siblings = candidate.parentElement.children;
					let siblingsWithUserLinks = 0;
					for (const sib of siblings) {
						if (sib !== candidate && sib.querySelector && sib.querySelector('a[href*="/user/profile/"]')) siblingsWithUserLinks++;
					}
					if (siblingsWithUserLinks > 0 && bestCandidate === null) { bestCandidate = candidate; break; }
				}
				candidate = candidate.parentElement;
			}

			if (!bestCandidate) continue;
			const itemKey = bestCandidate.innerHTML.substring(0, 100);
			if (seenItems.has(itemKey)) continue;
			seenItems.add(itemKey);

			const text = bestCandidate.innerText || '';
			const stripped = text.replace(/\s+/g, '');
			if (tabBarTexts.some(t => stripped === t)) continue;
			if (text.trim().length < 10) continue;

			notificationItems.push(bestCandidate);
		}

		const idx = %d;
		if (idx >= notificationItems.length) {
			return JSON.stringify({ clicked: false, index: idx, total: notificationItems.length, error: "index out of range" });
		}

		const targetItem = notificationItems[idx];
		targetItem.scrollIntoView({ block: 'center' });

		// 在条目内找"回复"文字按钮
		const allChildren = targetItem.querySelectorAll('*');
		for (let el of allChildren) {
			if (el.children.length === 0 && el.textContent.trim() === '回复') {
				el.click();
				return JSON.stringify({ clicked: true, index: idx, total: notificationItems.length });
			}
		}

		return JSON.stringify({ clicked: false, index: idx, total: notificationItems.length, error: "reply button not found in item" });
	}`, index))
	if err != nil {
		return fmt.Errorf("执行点击回复 JS 失败: %w", err)
	}

	raw := result.Value.Str()
	logrus.Infof("clickReplyButton 结果: %s", raw)

	if strings.Contains(raw, `"clicked":false`) || strings.Contains(raw, `"clicked": false`) {
		return fmt.Errorf("未找到第 %d 条回复按钮: %s", index, raw)
	}

	logrus.Infof("成功点击第 %d 条回复按钮", index)
	return nil
}

// inputReplyContent 在回复输入框中输入内容
func inputReplyContent(page *rod.Page, content string) error {
	// 查找回复输入框（通知页点击回复后通常会出现输入框）
	selectors := []string{
		"div.input-box div.content-edit p.content-input",
		"textarea[placeholder*='回复']",
		"div[contenteditable='true']",
		"input[placeholder*='回复']",
		"div.comment-input textarea",
		"div.reply-input textarea",
	}

	for _, sel := range selectors {
		elem, err := page.Timeout(3 * time.Second).Element(sel)
		if err == nil && elem != nil {
			logrus.Infof("找到回复输入框: %s", sel)
			if err := elem.Click(proto.InputMouseButtonLeft, 1); err != nil {
				logrus.Warnf("点击输入框失败: %v", err)
			}
			humanSleep(300 * time.Millisecond)
			if err := elem.Input(content); err != nil {
				logrus.Warnf("直接 Input 失败: %v，尝试 JS 方式", err)
				// 用 JS 方式注入文本
				_, jsErr := elem.Eval(fmt.Sprintf(`(el) => {
					el.innerText = %q;
					el.dispatchEvent(new InputEvent('input', { bubbles: true }));
				}`, content))
				if jsErr != nil {
					return fmt.Errorf("输入回复内容失败: %w", jsErr)
				}
			}
			logrus.Info("回复内容输入成功")
			return nil
		}
	}

	return fmt.Errorf("未找到回复输入框")
}

// clickSendButton 点击发送按钮
func clickSendButton(page *rod.Page) error {
	selectors := []string{
		"button.submit",
		"div.bottom button.submit",
		"button:has-text('发送')",
	}

	for _, sel := range selectors {
		elem, err := page.Timeout(3 * time.Second).Element(sel)
		if err == nil && elem != nil {
			if err := elem.Click(proto.InputMouseButtonLeft, 1); err != nil {
				logrus.Warnf("点击发送按钮失败 (%s): %v", sel, err)
				continue
			}
			logrus.Info("已点击发送按钮")
			return nil
		}
	}

	// 兜底 JS
	_, err := page.Eval(`() => {
		const buttons = document.querySelectorAll('button');
		for (let btn of buttons) {
			if (btn.textContent.trim() === '发送') {
				btn.click();
				return true;
			}
		}
		return false;
	}`)
	if err != nil {
		return fmt.Errorf("JS 点击发送按钮失败: %w", err)
	}

	logrus.Info("已通过 JS 点击发送按钮")
	return nil
}
