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
	humanSleep(2 * time.Second)

	// 切 tab 后等待新内容加载
	if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("切tab后等待DOM稳定出现问题: %v，继续尝试", err)
	}

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
	humanSleep(2 * time.Second)

	if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("切tab后等待DOM稳定出现问题: %v，继续尝试", err)
	}

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
	// 先检查页面是否已有评论内容（已在正确 tab）
	hasComments, _ := page.Eval(`() => {
		const text = document.body.innerText || '';
		return /(评论了你的笔记|回复了你的评论|@了你|在评论中@了你|提到了你)/.test(text);
	}`)
	if hasComments != nil && hasComments.Value.Bool() {
		logrus.Info("页面已有评论内容，已在「评论和@」tab")
		return nil
	}

	// 用 JS 精确点击「评论和@」tab 文字（尝试多种匹配策略）
	for attempt := 0; attempt < 3; attempt++ {
		logrus.Infof("尝试切换到「评论和@」tab (第%d次)", attempt+1)
		clicked, _ := page.Eval(`() => {
			// 策略1: 找所有叶子节点中文字为"评论和@"的
			const allEls = document.querySelectorAll('*');
			for (let el of allEls) {
				const t = el.textContent.trim();
				if (t === '评论和@' && el.offsetParent !== null) {
					el.click();
					return 'leaf';
				}
			}
			// 策略2: 找包含"评论和@"的可点击元素
			for (let el of allEls) {
				const t = el.textContent.trim();
				if (t === '评论和@' || t === '评论和@赞和收藏新增关注'.substring(0, 4)) {
					if (el.closest('a, button, [role="tab"], [class*="tab"]')) {
						el.closest('a, button, [role="tab"], [class*="tab"]').click();
						return 'closest';
					}
					el.click();
					return 'direct';
				}
			}
			return '';
		}`)
		if clicked != nil && clicked.Value.Str() != "" {
			logrus.Infof("已切换到「评论和@」tab (方式: %s)", clicked.Value.Str())
		}

		humanSleep(2 * time.Second)

		// 验证是否切换成功
		check, _ := page.Eval(`() => {
			const text = document.body.innerText || '';
			return /(评论了你的笔记|回复了你的评论|@了你|在评论中@了你|提到了你)/.test(text);
		}`)
		if check != nil && check.Value.Bool() {
			logrus.Info("确认已在「评论和@」tab（内容验证通过）")
			return nil
		}
		logrus.Warnf("第%d次切换后未检测到评论内容，重试", attempt+1)
	}

	logrus.Warn("3次尝试后仍未切换到评论tab，继续尝试提取")
	return fmt.Errorf("切换评论tab失败")
}

// extractNotificationComments 从通知页提取评论列表
// 核心策略：用 JS 找到每个包含用户链接的最小粒度通知条目，过滤掉 tab 栏等无关元素
func extractNotificationComments(page *rod.Page) ([]NotificationComment, error) {
	return extractCommentsViaJS(page)
}

// extractCommentsViaJS 通过 JS 提取评论信息
// 策略：自底向上找通知条目，结果按 user_id+content 去重（同一条通知的头像链接和文字链接会产生两条相同记录）
func extractCommentsViaJS(page *rod.Page) ([]NotificationComment, error) {
	result, err := page.Eval(`() => {
		const raw = [];
		const tabBarTexts = ['评论和@赞和收藏新增关注', '评论和@', '赞和收藏', '新增关注'];

		const userLinks = document.querySelectorAll('a[href*="/user/profile/"]');
		if (userLinks.length === 0) return JSON.stringify([]);

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
			const text = bestCandidate.innerText || '';
			const stripped = text.replace(/\s+/g, '');
			if (tabBarTexts.some(t => stripped === t)) continue;
			if (text.trim().length < 10) continue;

			// 提取 user_id（从用户链接的 href）
			const firstLink = bestCandidate.querySelector('a[href*="/user/profile/"]');
			let userId = '';
			if (firstLink) {
				const m = firstLink.href.match(/\/user\/profile\/([^?/]+)/);
				if (m) userId = m[1];
			}

			// 提取用户名：从通知文本第一行获取（头像链接没有文字）
			let username = '';
			const lines = text.split('\n').map(l => l.trim()).filter(l => l.length > 0);
			if (lines.length > 0) username = lines[0];

			let action = '';
			if (text.includes('评论了你的笔记')) action = '评论了你的笔记';
			else if (text.includes('回复了你的评论')) action = '回复了你的评论';
			else if (text.includes('@了你') || text.includes('在评论中@了你') || text.includes('提到了你')) action = '在评论中@了你';

			let time = '';
			const timeMatch = text.match(/(\d+天前|\d+小时前|\d+分钟前|昨天 \d{1,2}:\d{2}|前天 \d{1,2}:\d{2}|刚刚|\d{1,2}:\d{2})/);
			if (timeMatch) time = timeMatch[1];

			let content = text.trim();
			if (content.length > 300) content = content.substring(0, 300) + '...';

			raw.push({ username, user_id: userId, action, content, time, note_title: '', note_snippet: '' });
		}

		// 后处理去重：同一通知的头像链接和文字链接会生成两条 user_id+content 相同的记录
		const seen = new Set();
		const comments = [];
		for (const item of raw) {
			const key = item.user_id + '|' + item.content.substring(0, 100);
			if (seen.has(key)) continue;
			seen.add(key);
			// 优先保留有 username 的版本（文字链接那条）
			item.index = comments.length;
			comments.push(item);
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

		const userLinks = document.querySelectorAll('a[href*="/user/profile/"]');
		if (userLinks.length === 0) {
			return JSON.stringify({ clicked: false, error: "no user links found on page" });
		}

		// 自底向上找所有通知条目（含重复）
		const rawItems = [];
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
			const text = bestCandidate.innerText || '';
			const stripped = text.replace(/\s+/g, '');
			if (tabBarTexts.some(t => stripped === t)) continue;
			if (text.trim().length < 10) continue;

			const firstLink = bestCandidate.querySelector('a[href*="/user/profile/"]');
			let userId = '';
			if (firstLink) {
				const m = firstLink.href.match(/\/user\/profile\/([^?/]+)/);
				if (m) userId = m[1];
			}
			const content = (text.trim()).substring(0, 100);
			rawItems.push({ el: bestCandidate, key: userId + '|' + content });
		}

		// 按 user_id+content 去重，保留每组第一个 DOM 元素
		const seen = new Set();
		const notificationItems = [];
		for (const item of rawItems) {
			if (seen.has(item.key)) continue;
			seen.add(item.key);
			notificationItems.push(item.el);
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
			// 聚焦
			if err := elem.Click(proto.InputMouseButtonLeft, 1); err != nil {
				logrus.Warnf("点击输入框失败: %v", err)
			}
			elem.Eval(`function() { this.focus() }`)
			humanSleep(300 * time.Millisecond)

			// 用 CDP InsertText 输入（elem.Input 在 textarea 卡死，JS 设值不触发框架状态更新）
			if err := page.InsertText(content); err != nil {
				return fmt.Errorf("输入回复内容失败: %w", err)
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
