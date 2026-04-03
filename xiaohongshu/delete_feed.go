package xiaohongshu

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

const (
	// 创作者后台笔记管理页面（新版）
	urlOfNoteManagement = "https://creator.xiaohongshu.com/new/note-manager?source=official"
)

// NoteInfo 笔记基本信息
type NoteInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Views     string `json:"views"`     // 浏览量
	Comments  string `json:"comments"`  // 评论数
	Likes     string `json:"likes"`     // 点赞数
	Favorites string `json:"favorites"` // 收藏数
	Shares    string `json:"shares"`    // 转发数
}

// NoteManageAction 笔记管理操作（删除/置顶/权限设置）
type NoteManageAction struct {
	page *rod.Page
}

// NewNoteManageAction 创建笔记管理操作
func NewNoteManageAction(page *rod.Page) *NoteManageAction {
	return &NoteManageAction{page: page}
}

// navigateToNoteManager 导航到笔记管理页面并等待加载
func (n *NoteManageAction) navigateToNoteManager() error {
	page := n.page
	slog.Info("导航到笔记管理页面", "url", urlOfNoteManagement)
	if err := page.Navigate(urlOfNoteManagement); err != nil {
		return fmt.Errorf("导航到笔记管理页面失败: %w", err)
	}
	if err := page.WaitLoad(); err != nil {
		slog.Warn("等待页面加载", "error", err)
	}
	humanSleep(2 * time.Second)
	if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
		slog.Warn("等待 DOM 稳定", "error", err)
	}
	humanSleep(1 * time.Second)

	// 检查是否被重定向到登录页面
	info, err := page.Info()
	if err == nil && info != nil {
		url := info.URL
		if strings.Contains(url, "/login") || strings.Contains(url, "redirectReason=401") {
			return fmt.Errorf("创作者平台未登录（%s），主站 web_session 可能已失效", url)
		}
	}

	return nil
}

// findNoteRowAndClickButton 找到目标笔记行并点击指定操作按钮
// buttonText: "删除" | "置顶" | "权限设置"
func (n *NoteManageAction) findNoteRowAndClickButton(feedID, buttonText string) error {
	page := n.page
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		result, err := page.Eval(`(feedID, btnText) => {
			// 通过 data-impression 属性里的 noteId 定位笔记行
			let targetRow = null;
			const noteElems = document.querySelectorAll('[data-impression]');
			for (const el of noteElems) {
				const attr = el.getAttribute('data-impression');
				if (attr && attr.includes(feedID)) {
					targetRow = el;
					break;
				}
			}

			if (!targetRow) return {status: 'not_found'};

			// 按钮 class 映射（class="control data-del" 等）
			const btnClassMap = {'删除': 'data-del', '置顶': 'data-top', '权限设置': 'data-perm'};
			const btnClass = btnClassMap[btnText];
			let btn = null;
			if (btnClass) {
				btn = targetRow.querySelector('span.' + btnClass + ', span.control.' + btnClass);
			}
			// 兜底：文字精确匹配
			if (!btn) {
				const elems = targetRow.querySelectorAll('span, div, button');
				for (const el of elems) {
					if ((el.textContent || '').trim() === btnText && el.offsetParent !== null) {
						btn = el;
						break;
					}
				}
			}

			if (!btn) return {status: 'row_found_no_btn'};

			const rect = btn.getBoundingClientRect();
			return {status: 'found', x: rect.left + rect.width/2, y: rect.top + rect.height/2};
		}`, feedID, buttonText)

		if err != nil {
			humanSleep(500 * time.Millisecond)
			continue
		}

		status := result.Value.Get("status").String()
		slog.Info("笔记行查找结果", "status", status, "button", buttonText)

		if status == "found" {
			x := result.Value.Get("x").Num()
			y := result.Value.Get("y").Num()
			slog.Info("真实点击笔记操作按钮", "button", buttonText, "x", x, "y", y)
			if err := page.Mouse.MustMoveTo(x, y).Click(proto.InputMouseButtonLeft, 1); err != nil {
				humanSleep(500 * time.Millisecond)
				continue
			}
			return nil
		}

		if status == "row_found_no_btn" {
			return fmt.Errorf("找到笔记行但未找到「%s」按钮", buttonText)
		}

		humanSleep(500 * time.Millisecond)
	}
	return fmt.Errorf("超时：未找到笔记 %s 的「%s」按钮", feedID, buttonText)
}

// confirmDialog 确认弹窗（点击"确定"/"确认删除"等确认按钮）
func confirmDialog(page *rod.Page) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		result, err := page.Eval(`() => {
			const confirmTexts = ['确认', '确定', '确认删除', '确认置顶'];
			// 先在弹窗范围内找，再全局找
			const scopes = [
				document.querySelector('.d-modal, .d-dialog, [class*="modal"], [class*="dialog"], [class*="popup"]'),
				document.body,
			];
			for (const scope of scopes) {
				if (!scope) continue;
				const btns = scope.querySelectorAll('button, .d-button, [class*="btn"]');
				for (const btn of btns) {
					const text = (btn.textContent || '').trim();
					if (!confirmTexts.includes(text)) continue;
					const rect = btn.getBoundingClientRect();
					// 用 rect 判断可见（fixed 元素 offsetParent===null 但 rect 有值）
					if (rect.width > 0 && rect.height > 0) {
						return {found: true, x: rect.left + rect.width/2, y: rect.top + rect.height/2, text};
					}
				}
			}
			return {found: false};
		}`)
		if err != nil {
			humanSleep(300 * time.Millisecond)
			continue
		}
		if result.Value.Get("found").Bool() {
			x := result.Value.Get("x").Num()
			y := result.Value.Get("y").Num()
			text := result.Value.Get("text").String()
			slog.Info("点击确认按钮", "text", text)
			return page.Mouse.MustMoveTo(x, y).Click(proto.InputMouseButtonLeft, 1)
		}
		humanSleep(300 * time.Millisecond)
	}
	return fmt.Errorf("超时：未找到确认按钮")
}

// Delete 删除笔记：点击删除按钮 → 确认弹窗 → 验证消失
func (n *NoteManageAction) Delete(ctx context.Context, feedID string) error {
	n.page = n.page.Context(ctx).Timeout(120 * time.Second)
	if err := n.navigateToNoteManager(); err != nil {
		return err
	}
	if err := n.findNoteRowAndClickButton(feedID, "删除"); err != nil {
		return fmt.Errorf("点击删除按钮失败: %w", err)
	}
	humanSleep(800 * time.Millisecond)
	if err := confirmDeleteDialog(n.page); err != nil {
		return fmt.Errorf("确认删除失败: %w", err)
	}
	// 等待页面刷新，验证笔记已消失
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		humanSleep(1 * time.Second)
		res, err := n.page.Eval(`(feedID) => {
			for (const el of document.querySelectorAll('[data-impression]')) {
				if ((el.getAttribute('data-impression') || '').includes(feedID)) return true;
			}
			return false;
		}`, feedID)
		if err != nil || !res.Value.Bool() {
			slog.Info("笔记删除成功", "feed_id", feedID)
			return nil
		}
	}
	return fmt.Errorf("删除操作执行后笔记仍存在，可能未成功")
}

// confirmDeleteDialog 专门用于删除确认弹窗：必须找到含"删除"字样的弹窗再点确认
func confirmDeleteDialog(page *rod.Page) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		result, err := page.Eval(`() => {
			// 找含"删除"字样的弹窗
			const allModals = document.querySelectorAll('.d-modal, .d-dialog, [class*="modal"], [class*="dialog"], [class*="popup"]');
			let deleteModal = null;
			for (const m of allModals) {
				if ((m.textContent || '').includes('删除')) { deleteModal = m; break; }
			}
			const scope = deleteModal || document.body;
			const confirmTexts = ['确认', '确定', '确认删除'];
			const btns = scope.querySelectorAll('button, .d-button, [class*="btn"]');
			for (const btn of btns) {
				const text = (btn.textContent || '').trim();
				if (!confirmTexts.includes(text)) continue;
				const rect = btn.getBoundingClientRect();
				if (rect.width > 0 && rect.height > 0) {
					return {found: true, x: rect.left + rect.width/2, y: rect.top + rect.height/2, text, hasModal: !!deleteModal};
				}
			}
			return {found: false};
		}`)
		if err != nil {
			humanSleep(300 * time.Millisecond)
			continue
		}
		if result.Value.Get("found").Bool() {
			x := result.Value.Get("x").Num()
			y := result.Value.Get("y").Num()
			text := result.Value.Get("text").String()
			hasModal := result.Value.Get("hasModal").Bool()
			slog.Info("点击删除确认按钮", "text", text, "hasModal", hasModal)
			return page.Mouse.MustMoveTo(x, y).Click(proto.InputMouseButtonLeft, 1)
		}
		humanSleep(300 * time.Millisecond)
	}
	return fmt.Errorf("超时：未找到删除确认按钮")
}

// getNoteVisibility 读取笔记当前的可见范围文字（从笔记行里提取）
func (n *NoteManageAction) getNoteVisibility(feedID string) string {
	res, err := n.page.Eval(`(feedID) => {
		for (const el of document.querySelectorAll('[data-impression]')) {
			const attr = el.getAttribute('data-impression');
			if (!attr || !attr.includes(feedID)) continue;
			// 可见范围标签通常含"可见"二字，且是短文本
			const spans = el.querySelectorAll('span, div');
			for (const s of spans) {
				const t = (s.textContent || '').trim();
				if (t.includes('可见') && t.length < 15) return t;
			}
		}
		return '';
	}`, feedID)
	if err != nil {
		return ""
	}
	return res.Value.String()
}

// Pin 置顶笔记：点击置顶按钮 → 确认弹窗
// 注意：仅自己可见 / 仅互关好友可见 的笔记不允许置顶
func (n *NoteManageAction) Pin(ctx context.Context, feedID string) error {
	n.page = n.page.Context(ctx).Timeout(120 * time.Second)
	if err := n.navigateToNoteManager(); err != nil {
		return err
	}
	if vis := n.getNoteVisibility(feedID); vis == "仅自己可见" || vis == "仅互关好友可见" {
		return fmt.Errorf("笔记当前权限为「%s」，不支持置顶，请先将权限改为公开可见", vis)
	}
	if err := n.findNoteRowAndClickButton(feedID, "置顶"); err != nil {
		return fmt.Errorf("点击置顶按钮失败: %w", err)
	}
	humanSleep(800 * time.Millisecond)
	if err := confirmDialog(n.page); err != nil {
		return fmt.Errorf("确认置顶失败: %w", err)
	}
	slog.Info("笔记置顶成功", "feed_id", feedID)
	return nil
}

// SetVisibility 设置权限：点击权限设置 → 选择可见范围 → 点击确定
func (n *NoteManageAction) SetVisibility(ctx context.Context, feedID, visibility string) error {
	n.page = n.page.Context(ctx).Timeout(120 * time.Second)
	if err := n.navigateToNoteManager(); err != nil {
		return err
	}
	if err := n.findNoteRowAndClickButton(feedID, "权限设置"); err != nil {
		return fmt.Errorf("点击权限设置按钮失败: %w", err)
	}
	humanSleep(1 * time.Second) // 等弹窗出现

	// 在弹窗里找"可见范围"下拉并选择
	if err := n.selectVisibilityInDialog(visibility); err != nil {
		return fmt.Errorf("设置可见范围失败: %w", err)
	}
	humanSleep(500 * time.Millisecond)

	// 点击确定
	if err := confirmDialog(n.page); err != nil {
		return fmt.Errorf("确认权限设置失败: %w", err)
	}
	slog.Info("权限设置成功", "feed_id", feedID, "visibility", visibility)
	return nil
}

// selectVisibilityInDialog 在权限设置弹窗里操作下拉选择可见范围
func (n *NoteManageAction) selectVisibilityInDialog(visibility string) error {
	page := n.page
	slog.Info("设置可见范围", "visibility", visibility)

	// 点击下拉框触发器（优先点 d-select-suffix 箭头）
	result, err := page.Eval(`() => {
		const modal = document.querySelector('.d-modal, .d-dialog, [class*="modal"], [class*="dialog"]');
		const scope = modal || document.body;
		// 尝试找箭头（suffix），兜底用整个 d-select
		const suffix = scope.querySelector('.d-select-suffix');
		const main = scope.querySelector('.d-select-main, .d-select');
		const trigger = suffix || main;
		if (trigger) {
			const rect = trigger.getBoundingClientRect();
			return {found: true, x: rect.left + rect.width/2, y: rect.top + rect.height/2, tag: trigger.className};
		}
		return {found: false};
	}`)
	if err != nil || !result.Value.Get("found").Bool() {
		return fmt.Errorf("未找到可见范围下拉框")
	}

	x := result.Value.Get("x").Num()
	y := result.Value.Get("y").Num()
	slog.Info("点击下拉触发器", "x", x, "y", y, "class", result.Value.Get("tag").String())
	if err := page.Mouse.MustMoveTo(x, y).Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("点击下拉框失败: %w", err)
	}
	humanSleep(1 * time.Second) // 等待下拉选项出现

	// 最多重试 3 次（每次重新点开下拉 + 选项）
	// DOM: #d-portal > .d-popover.d-dropdown > .d-dropdown-wrapper > .d-dropdown-content > .d-options-wrapper > [选项]
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			// 重新点开下拉
			slog.Info("重新点击下拉触发器", "attempt", attempt+1)
			if err := page.Mouse.MustMoveTo(x, y).Click(proto.InputMouseButtonLeft, 1); err != nil {
				humanSleep(500 * time.Millisecond)
				continue
			}
			humanSleep(800 * time.Millisecond)
		}

		// 找最小的、精确匹配目标文字的叶节点并点击
		res, err := page.Eval(`(vis) => {
			const wrapper = document.querySelector('#d-portal .d-options-wrapper, .d-options-wrapper, .d-dropdown-content');
			if (!wrapper) return {found: false, debug: 'no wrapper'};

			// 找所有文字完全匹配的元素，优先取最小的（叶节点或子元素少的）
			const candidates = [];
			for (const el of wrapper.querySelectorAll('*')) {
				const text = (el.textContent || '').trim();
				if (text !== vis) continue;
				const rect = el.getBoundingClientRect();
				if (rect.width > 0 && rect.height > 0) {
					candidates.push({el, rect, childCount: el.children.length});
				}
			}
			if (candidates.length === 0) {
				const texts = [...new Set(Array.from(wrapper.querySelectorAll('*'))
					.map(e => (e.textContent||'').trim()).filter(t => t.length > 0 && t.length < 30))];
				return {found: false, debug: JSON.stringify(texts.slice(0, 10))};
			}
			// 子元素最少的（最接近叶节点）优先
			candidates.sort((a, b) => a.childCount - b.childCount);
			const {rect} = candidates[0];
			return {found: true, x: rect.left + rect.width/2, y: rect.top + rect.height/2};
		}`, visibility)
		if err != nil {
			humanSleep(300 * time.Millisecond)
			continue
		}
		if !res.Value.Get("found").Bool() {
			slog.Info("选项搜索debug", "items", res.Value.Get("debug").String())
			humanSleep(300 * time.Millisecond)
			continue
		}

		ox := res.Value.Get("x").Num()
		oy := res.Value.Get("y").Num()
		slog.Info("点击可见范围选项", "visibility", visibility, "attempt", attempt+1)
		if err := page.Mouse.MustMoveTo(ox, oy).Click(proto.InputMouseButtonLeft, 1); err != nil {
			humanSleep(300 * time.Millisecond)
			continue
		}
		humanSleep(500 * time.Millisecond)

		// 验证 select 当前值是否已变为目标值
		vres, err := page.Eval(`(vis) => {
			const modal = document.querySelector('.d-modal, .d-dialog, [class*="modal"], [class*="dialog"]');
			const scope = modal || document.body;
			const main = scope.querySelector('.d-select-main, .d-select-selection, [class*="select-value"]');
			const current = main ? (main.textContent || '').trim() : '';
			return {current, ok: current.includes(vis)};
		}`, visibility)
		if err == nil {
			current := vres.Value.Get("current").String()
			ok := vres.Value.Get("ok").Bool()
			slog.Info("验证选项值", "current", current, "target", visibility, "ok", ok)
			if ok {
				return nil
			}
		}
		// 没变成功，下一轮重试
	}
	return fmt.Errorf("未能选中可见范围选项: %s", visibility)
}

// ListNotes 列出笔记管理页所有笔记（ID + 标题 + 统计数据）
func (n *NoteManageAction) ListNotes(ctx context.Context) ([]NoteInfo, error) {
	n.page = n.page.Context(ctx).Timeout(60 * time.Second)
	if err := n.navigateToNoteManager(); err != nil {
		return nil, err
	}

	result, err := n.page.Eval(`() => {
		const notes = [];
		const seen = new Set();
		document.querySelectorAll('[data-impression]').forEach(el => {
			try {
				const d = JSON.parse(el.getAttribute('data-impression'));
				const id = d?.noteTarget?.value?.noteId;
				if (!id || seen.has(id)) return;
				seen.add(id);

				// 找标题
				const titleEl = el.querySelector('[class*="title"], [class*="name"], a, span');
				const title = titleEl ? (titleEl.textContent || '').trim().substring(0, 50) : '';

				// 找包含统计数字的容器（通常在 data-impression 元素内或其父元素内）
				// 统计顺序：浏览量、评论、点赞、收藏、转发
				let views = '', comments = '', likes = '', favorites = '', shares = '';
				const container = el.closest('li, [class*="note-item"], [class*="item"], tr') || el;
				// 找所有含数字的 span/div，过滤掉标题文字
				const statNums = [];
				container.querySelectorAll('span, em, i').forEach(s => {
					const txt = (s.textContent || '').trim();
					if (/^\d+$/.test(txt)) {
						statNums.push(txt);
					}
				});
				// 也尝试从 data-impression 里拿统计（部分页面会把数据放进去）
				const statsFromAttr = d?.noteTarget?.value;
				if (statsFromAttr) {
					views     = String(statsFromAttr.viewCount     ?? statsFromAttr.readCount     ?? '');
					comments  = String(statsFromAttr.commentCount  ?? '');
					likes     = String(statsFromAttr.likeCount     ?? statsFromAttr.likedCount    ?? '');
					favorites = String(statsFromAttr.collectCount  ?? statsFromAttr.favoriteCount ?? '');
					shares    = String(statsFromAttr.shareCount    ?? '');
				}
				// 如果属性里拿不到，退而从 DOM 数字列表按顺序取（5 个数字）
				if (!views && statNums.length >= 5) {
					[views, comments, likes, favorites, shares] = statNums.slice(0, 5);
				}

				notes.push({id, title, views, comments, likes, favorites, shares});
			} catch(e) {}
		});
		return notes;
	}`)
	if err != nil {
		return nil, fmt.Errorf("获取笔记列表失败: %w", err)
	}
	var notes []NoteInfo
	for _, v := range result.Value.Arr() {
		id := v.Get("id").String()
		title := v.Get("title").String()
		if id != "" {
			notes = append(notes, NoteInfo{
				ID:        id,
				Title:     title,
				Views:     v.Get("views").String(),
				Comments:  v.Get("comments").String(),
				Likes:     v.Get("likes").String(),
				Favorites: v.Get("favorites").String(),
				Shares:    v.Get("shares").String(),
			})
		}
	}
	return notes, nil
}

// ListNoteIDs 向后兼容：只返回 ID 列表
func (n *NoteManageAction) ListNoteIDs(ctx context.Context) ([]string, error) {
	notes, err := n.ListNotes(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, note := range notes {
		if s := note.ID; s != "" {
			ids = append(ids, s)
		}
	}
	return ids, nil
}

// --- 保持向后兼容的旧名称 ---

// DeleteAction 向后兼容别名
type DeleteAction = NoteManageAction

// NewDeleteAction 向后兼容
func NewDeleteAction(page *rod.Page) *NoteManageAction {
	return NewNoteManageAction(page)
}
