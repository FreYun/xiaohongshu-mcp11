package xiaohongshu

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// PublishLongformContent 长文发布内容
type PublishLongformContent struct {
	Title      string   // 标题
	Content    string   // 正文（支持多段落，段落间用 \n 分隔）
	Desc       string   // 发布页描述（可选，不填则使用正文前 800 字）
	Tags       []string // 话题标签（可选），如 ["美食", "旅行"]
	Visibility string   // 可见范围: "公开可见"(默认), "仅自己可见", "仅互关好友可见"
}

// PublishLongformAction 长文发布操作
type PublishLongformAction struct {
	page *rod.Page
}

// NewPublishLongformAction 创建长文发布操作（导航到发布页并切换到「写长文」）
func NewPublishLongformAction(page *rod.Page) (*PublishLongformAction, error) {
	pp := page.Timeout(300 * time.Second)

	// 导航到创作者发布页
	if err := pp.Navigate(urlOfPublic); err != nil {
		return nil, errors.Wrap(err, "导航到发布页面失败")
	}

	if err := pp.WaitLoad(); err != nil {
		logrus.Warnf("等待页面加载出现问题: %v，继续尝试", err)
	}
	humanSleep(2 * time.Second)

	if err := pp.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("等待 DOM 稳定出现问题: %v，继续尝试", err)
	}
	humanSleep(1 * time.Second)

	// 点击「写长文」tab
	if err := mustClickPublishTab(pp, "写长文"); err != nil {
		return nil, errors.Wrap(err, "点击写长文 TAB 失败")
	}
	humanSleep(1 * time.Second)

	// 点击「新的创作」按钮
	if err := clickNewCreation(pp); err != nil {
		return nil, errors.Wrap(err, "点击新的创作失败")
	}

	// 等待编辑器页面加载（会跳转到新页面）
	humanSleep(3 * time.Second)
	if err := pp.WaitLoad(); err != nil {
		logrus.Warnf("等待编辑器页面加载: %v，继续", err)
	}
	if err := pp.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("等待编辑器 DOM 稳定: %v，继续", err)
	}
	humanSleep(2 * time.Second)

	return &PublishLongformAction{page: pp}, nil
}

// PublishLongform 执行长文发布全流程
func (p *PublishLongformAction) PublishLongform(ctx context.Context, content PublishLongformContent) error {
	page := p.page.Context(ctx)

	// 1. 填写标题
	if err := inputLongformTitle(page, content.Title); err != nil {
		return errors.Wrap(err, "输入长文标题失败")
	}
	slog.Info("长文标题填写完成", "title", content.Title)
	humanSleep(1 * time.Second)

	// 2. 填写正文（用 execCommand）
	if err := inputLongformBody(page, content.Content); err != nil {
		return errors.Wrap(err, "输入长文正文失败")
	}
	slog.Info("长文正文填写完成")
	humanSleep(1 * time.Second)

	// 3. 点击「一键排版」
	if err := clickOneClickLayout(page); err != nil {
		return errors.Wrap(err, "点击一键排版失败")
	}
	slog.Info("已点击一键排版")

	// 等待预览/模板页面加载
	humanSleep(3 * time.Second)
	if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("等待排版页面 DOM 稳定: %v，继续", err)
	}
	humanSleep(1 * time.Second)

	// 4. 选择第一个排版模板（点击预览区域的模板卡片）
	if err := selectFirstTemplate(page); err != nil {
		logrus.Warnf("选择排版模板失败: %v，继续尝试下一步", err)
	}
	humanSleep(1 * time.Second)

	// 5. 点击「下一步」
	if err := clickNextStep(page); err != nil {
		return errors.Wrap(err, "点击下一步失败")
	}
	slog.Info("已点击下一步")
	humanSleep(2 * time.Second)

	// 6. 等待最终发布页加载
	humanSleep(3 * time.Second)
	if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("等待发布页 DOM 稳定: %v，继续", err)
	}
	humanSleep(1 * time.Second)

	// 7. 填写发布页描述
	desc := content.Desc
	if desc == "" {
		desc = truncateDesc(content.Content, 800)
	}
	if err := inputPublishDesc(page, desc); err != nil {
		logrus.Warnf("输入发布描述失败: %v", err)
	}
	humanSleep(1 * time.Second)

	// 7.5 输入话题标签（参考文字配图流程：找 textbox → JS 光标定位末尾 → 回车 → 逐个 inputTag）
	if len(content.Tags) > 0 {
		tagSelectors := []string{
			`div[contenteditable="true"][role="textbox"]`,
			`div.ql-editor`,
			`div[contenteditable="true"]`,
		}
		var tagElem *rod.Element
		for _, sel := range tagSelectors {
			els, err := page.Elements(sel)
			if err != nil || len(els) == 0 {
				continue
			}
			// 取最后一个可见的 contenteditable 元素
			el := els[len(els)-1]
			if visible, _ := el.Visible(); !visible {
				continue
			}
			tagElem = el
			break
		}
		if tagElem == nil {
			logrus.Warn("长文发布页未找到内容编辑器，跳过标签输入")
		} else {
			// 全部用 JS 输入标签，避免 rod 元素引用失效
			focusCursorToEnd := func() {
				_, _ = page.Eval(`() => {
					const el = document.querySelector('[contenteditable="true"][role="textbox"]') || document.querySelector('div.ql-editor') || document.querySelector('[contenteditable="true"]');
					if (el) { el.focus(); }
					const sel = window.getSelection();
					if (el) {
						const range = document.createRange();
						range.selectNodeContents(el);
						range.collapse(false);
						sel.removeAllRanges();
						sel.addRange(range);
					}
				}`)
				humanSleep(300 * time.Millisecond)
			}

			// 首次聚焦并回车换行
			focusCursorToEnd()
			_, _ = page.Eval(`() => { document.execCommand('insertLineBreak'); }`)
			humanSleep(300 * time.Millisecond)

			// 逐个输入标签
			allOK := true
			for _, tag := range content.Tags {
				tag = strings.TrimLeft(tag, "#")
				// 聚焦 + 光标到末尾
				focusCursorToEnd()
				// 用 JS execCommand 输入 # 和标签文字
				_, _ = page.Eval(`() => { document.execCommand('insertText', false, '#'); }`)
				humanSleep(200 * time.Millisecond)
				for _, ch := range tag {
					_, _ = page.Eval(`(c) => { document.execCommand('insertText', false, c); }`, string(ch))
					humanSleep(50 * time.Millisecond)
				}
				humanSleep(1 * time.Second)
				// 点击标签联想选项
				topicContainer, err := page.Timeout(5 * time.Second).Element("#creator-editor-topic-container")
				if err == nil && topicContainer != nil {
					firstItem, err := topicContainer.Element(".item")
					if err == nil && firstItem != nil {
						if err := firstItem.Click(proto.InputMouseButtonLeft, 1); err == nil {
							slog.Info("长文标签联想点击成功", "tag", tag)
						}
					} else {
						// 没有联想，输入空格结束
						_, _ = page.Eval(`() => { document.execCommand('insertText', false, ' '); }`)
						slog.Warn("长文标签无联想，已空格结束", "tag", tag)
					}
				} else {
					_, _ = page.Eval(`() => { document.execCommand('insertText', false, ' '); }`)
					slog.Warn("长文标签无联想容器，已空格结束", "tag", tag)
				}
				humanSleep(500 * time.Millisecond)
			}
			if allOK {
				slog.Info("长文标签输入完成", "tags", content.Tags)
			}
		}
		humanSleep(1 * time.Second)
	}

	// 8. 设置可见范围（与图文一致）
	if err := setVisibility(page, content.Visibility); err != nil {
		logrus.Warnf("设置长文可见范围失败: %v", err)
	}
	humanSleep(500 * time.Millisecond)

	// 9. 点击发布
	if err := clickPublishButton(page); err != nil {
		return errors.Wrap(err, "点击发布按钮失败")
	}

	humanSleep(3 * time.Second)
	slog.Info("长文发布完成")
	return nil
}

// clickNewCreation 点击「新的创作」按钮
func clickNewCreation(page *rod.Page) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// 查找包含「新的创作」的按钮或链接
		elems, err := page.Elements("button, a, div.new-creation, [class*=new-creation], [class*=create]")
		if err == nil {
			for _, elem := range elems {
				text, _ := elem.Text()
				if strings.Contains(text, "新的创作") {
					if err := elem.Click(proto.InputMouseButtonLeft, 1); err == nil {
						logrus.Info("已点击「新的创作」按钮")
						return nil
					}
				}
			}
		}
		humanSleep(500 * time.Millisecond)
	}

	// 兜底 JS
	_, err := page.Eval(`() => {
		const elems = document.querySelectorAll('button, a, div, span');
		for (let el of elems) {
			if (el.textContent.trim() === '新的创作' || el.textContent.trim().includes('新的创作')) {
				el.click();
				return true;
			}
		}
		return false;
	}`)
	if err != nil {
		return fmt.Errorf("点击新的创作失败: %w", err)
	}
	return nil
}

// inputLongformTitle 输入长文标题
func inputLongformTitle(page *rod.Page, title string) error {
	// 长文编辑器的标题是 contenteditable 元素，placeholder="输入标题"
	selectors := []string{
		`[placeholder="输入标题"]`,
		`[data-placeholder="输入标题"]`,
		`h1[contenteditable="true"]`,
		`div.title[contenteditable="true"]`,
		`[class*="title"][contenteditable="true"]`,
	}

	var titleElem *rod.Element
	for _, sel := range selectors {
		elem, err := page.Timeout(10 * time.Second).Element(sel)
		if err == nil && elem != nil {
			titleElem = elem
			logrus.Infof("找到标题元素: %s", sel)
			break
		}
	}

	if titleElem == nil {
		return fmt.Errorf("未找到标题输入框")
	}

	// 先尝试普通 input 方式（如果是 input 元素）
	tagResult, _ := titleElem.Eval(`() => this.tagName.toLowerCase()`)
	tag := ""
	if tagResult != nil {
		tag = tagResult.Value.Str()
	}

	if tag == "input" {
		// 普通 input：用 value + input event
		_, err := titleElem.Eval(fmt.Sprintf(`(el) => {
			const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
			setter.call(el, %q);
			el.dispatchEvent(new Event('input', { bubbles: true }));
		}`, title))
		if err == nil {
			return nil
		}
	}

	// contenteditable 元素：先点击聚焦，再用 execCommand
	if err := titleElem.Click(proto.InputMouseButtonLeft, 1); err != nil {
		logrus.Warnf("点击标题元素失败: %v", err)
	}
	humanSleep(500 * time.Millisecond)

	_, err := page.Eval(fmt.Sprintf(`() => {
		document.execCommand('selectAll', false, null);
		document.execCommand('insertText', false, %q);
	}`, title))
	if err != nil {
		return fmt.Errorf("execCommand 输入标题失败: %w", err)
	}

	return nil
}

// inputLongformBody 输入长文正文（必须用 execCommand）
func inputLongformBody(page *rod.Page, content string) error {
	// 正文编辑器在标题下方，placeholder 为 "粘贴到这里或输入文字"
	editorSelectors := []string{
		`[placeholder="粘贴到这里或输入文字"]`,
		`[data-placeholder="粘贴到这里或输入文字"]`,
		"div.ql-editor",
		"div.ProseMirror",
		"div[contenteditable='true']:not(h1):not([placeholder='输入标题'])",
	}

	var editor *rod.Element
	for _, sel := range editorSelectors {
		elem, err := page.Timeout(5 * time.Second).Element(sel)
		if err == nil && elem != nil {
			editor = elem
			logrus.Infof("找到正文编辑器: %s", sel)
			break
		}
	}

	// 兜底：找页面上第二个 contenteditable（第一个是标题）
	if editor == nil {
		elems, err := page.Elements("div[contenteditable='true'], [contenteditable='true']")
		if err == nil && len(elems) >= 2 {
			editor = elems[1]
			logrus.Info("找到正文编辑器: 第二个 contenteditable 元素")
		}
	}

	if editor == nil {
		return fmt.Errorf("未找到长文编辑器")
	}

	// 先点击编辑器聚焦
	if err := editor.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("点击编辑器失败: %w", err)
	}
	humanSleep(500 * time.Millisecond)

	// 按段落（\n\n）拆分，段落间用 insertParagraph；段落内按行（\n）用 insertLineBreak
	_, err := page.Eval(`(content) => {
		const paragraphs = (content || '').split('\n\n');
		for (let p = 0; p < paragraphs.length; p++) {
			if (p > 0) document.execCommand('insertParagraph');
			const lines = paragraphs[p].split('\n');
			for (let i = 0; i < lines.length; i++) {
				document.execCommand('insertText', false, lines[i]);
				if (i < lines.length - 1) document.execCommand('insertLineBreak');
			}
		}
	}`, content)
	if err != nil {
		return fmt.Errorf("execCommand 输入正文失败: %w", err)
	}

	return nil
}

// clickOneClickLayout 点击「一键排版」按钮
func clickOneClickLayout(page *rod.Page) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		elems, err := page.Elements("button, span, div")
		if err == nil {
			for _, elem := range elems {
				text, _ := elem.Text()
				if strings.TrimSpace(text) == "一键排版" {
					if err := elem.Click(proto.InputMouseButtonLeft, 1); err == nil {
						return nil
					}
				}
			}
		}
		humanSleep(500 * time.Millisecond)
	}

	// 兜底 JS
	result, err := page.Eval(`() => {
		const elems = document.querySelectorAll('button, span, div, a');
		for (let el of elems) {
			if (el.children.length === 0 && el.textContent.trim() === '一键排版') {
				el.click();
				return true;
			}
		}
		return false;
	}`)
	if err != nil || !result.Value.Bool() {
		return fmt.Errorf("未找到一键排版按钮")
	}
	return nil
}

// selectFirstTemplate 选择第一个排版模板
func selectFirstTemplate(page *rod.Page) error {
	// 等待模板列表出现
	humanSleep(2 * time.Second)

	// 尝试点击第一个模板
	selectors := []string{
		".template-item:first-child",
		".template-list .item:first-child",
		"[class*=template] [class*=item]:first-child",
		".style-item:first-child",
	}

	for _, sel := range selectors {
		elem, err := page.Timeout(3 * time.Second).Element(sel)
		if err == nil && elem != nil {
			if err := elem.Click(proto.InputMouseButtonLeft, 1); err == nil {
				logrus.Info("已选择排版模板")
				return nil
			}
		}
	}

	// 兜底：直接点击第一个模板样式元素
	_, err := page.Eval(`() => {
		const items = document.querySelectorAll('[class*="template"] [class*="item"], [class*="style-item"]');
		if (items.length > 0) {
			items[0].click();
			return true;
		}
		return false;
	}`)
	if err != nil {
		logrus.Warnf("选择排版模板失败: %v", err)
	}
	return nil
}

// clickNextStep 点击「下一步」按钮
func clickNextStep(page *rod.Page) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		elems, err := page.Elements("button")
		if err == nil {
			for _, elem := range elems {
				text, _ := elem.Text()
				trimmed := strings.TrimSpace(text)
				if trimmed == "下一步" || trimmed == "下一步，去发布" {
					if disabled, _ := elem.Attribute("disabled"); disabled != nil {
						continue
					}
					if err := elem.Click(proto.InputMouseButtonLeft, 1); err == nil {
						return nil
					}
				}
			}
		}
		humanSleep(500 * time.Millisecond)
	}

	return fmt.Errorf("未找到下一步按钮")
}

// inputPublishDesc 输入发布页描述
func inputPublishDesc(page *rod.Page, desc string) error {
	// 发布页描述框 placeholder 为 "输入正文描述，真诚有价值的分享予人温暖"
	// 结构与普通图文发布页一致，是 ql-editor 或带 placeholder 的 textbox
	editorSelectors := []string{
		`[placeholder*="输入正文描述"]`,
		`[data-placeholder*="输入正文描述"]`,
		`p[data-placeholder*="输入正文描述"]`,
		"div.ql-editor",
	}

	for _, sel := range editorSelectors {
		elem, err := page.Timeout(5 * time.Second).Element(sel)
		if err == nil && elem != nil {
			logrus.Infof("找到描述输入框: %s", sel)

			// 如果找到的是 p 元素，向上找 textbox 父元素
			role, _ := elem.Attribute("role")
			if role == nil || *role != "textbox" {
				parent := findTextboxParent(elem)
				if parent != nil {
					elem = parent
				}
			}

			if err := elem.Click(proto.InputMouseButtonLeft, 1); err != nil {
				logrus.Warnf("点击描述框失败: %v", err)
				continue
			}
			humanSleep(300 * time.Millisecond)

			// 优先用 rod Input
			if err := elem.Input(desc); err == nil {
				logrus.Info("发布描述填写完成")
				return nil
			}

			// 兜底 execCommand
			_, err = page.Eval(fmt.Sprintf(`() => {
				document.execCommand('insertText', false, %q);
			}`, desc))
			if err == nil {
				logrus.Info("发布描述填写完成(execCommand)")
				return nil
			}
		}
	}

	return fmt.Errorf("未找到描述输入框")
}

// clickPublishButton 点击发布按钮
func clickPublishButton(page *rod.Page) error {
	// 尝试找发布按钮
	selectors := []string{
		".publish-page-publish-btn button.bg-red",
		"button.publish-btn",
	}

	for _, sel := range selectors {
		elem, err := page.Timeout(5 * time.Second).Element(sel)
		if err == nil && elem != nil {
			if err := elem.Click(proto.InputMouseButtonLeft, 1); err == nil {
				logrus.Info("已点击发布按钮")
				return nil
			}
		}
	}

	// 兜底：遍历所有 button，找文案为「发布」的
	elems, err := page.Elements("button")
	if err == nil {
		for _, elem := range elems {
			text, _ := elem.Text()
			if strings.TrimSpace(text) == "发布" || strings.TrimSpace(text) == "立即发布" {
				if err := elem.Click(proto.InputMouseButtonLeft, 1); err == nil {
					logrus.Info("已点击发布按钮")
					return nil
				}
			}
		}
	}

	return fmt.Errorf("未找到发布按钮")
}

// truncateDesc 截断描述到指定长度（按字符）
func truncateDesc(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen])
}
