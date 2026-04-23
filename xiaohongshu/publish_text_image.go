package xiaohongshu

import (
	"context"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// PublishTextImageContent 文字配图发布内容
type PublishTextImageContent struct {
	Title        string
	Content      string   // 图下正文
	TextCards    []string // 每张卡片的文字内容（最多3张）
	ImageStyle   string   // 卡片样式
	Tags         []string
	IsOriginal   bool
	Visibility   string
	ScheduleTime *time.Time
}

// PublishTextImageAction 文字配图发布操作
type PublishTextImageAction struct {
	page *rod.Page
}

// NewPublishTextImageAction 导航到发布页并切换到"上传图文" tab
func NewPublishTextImageAction(page *rod.Page) (*PublishTextImageAction, error) {
	pp := page.Timeout(300 * time.Second)

	if err := pp.Navigate(urlOfPublic); err != nil {
		return nil, errors.Wrap(err, "导航到发布页面失败")
	}
	if err := pp.WaitLoad(); err != nil {
		logrus.Warnf("等待页面加载: %v", err)
	}
	humanSleep(2 * time.Second)

	if err := pp.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("等待 DOM 稳定: %v", err)
	}
	humanSleep(1 * time.Second)

	if err := checkCreatorPageLogin(pp); err != nil {
		return nil, err
	}

	if err := mustClickPublishTab(pp, "上传图文"); err != nil {
		return nil, errors.Wrap(err, "点击上传图文 TAB 失败")
	}
	humanSleep(1 * time.Second)

	return &PublishTextImageAction{page: pp}, nil
}

// Publish 执行文字配图发布全流程
func (p *PublishTextImageAction) Publish(ctx context.Context, content PublishTextImageContent) error {
	page := p.page.Context(ctx)

	if len(content.TextCards) == 0 {
		return errors.New("文字卡片内容不能为空")
	}
	if len(content.TextCards) > 3 {
		return errors.New("最多支持3张文字卡片")
	}

	// 1. 点击"文字配图"按钮
	logrus.Info("text_to_image: 点击文字配图按钮")
	if err := clickByText(page, "文字配图"); err != nil {
		return errors.Wrap(err, "点击文字配图按钮失败")
	}
	humanSleep(1 * time.Second)

	// 2. 输入第一张卡片文字
	logrus.Info("text_to_image: 输入第一张卡片")
	if err := inputFirstCard(page, content.TextCards[0]); err != nil {
		return errors.Wrap(err, "输入第一张卡片失败")
	}

	// 3. 添加更多卡片
	for i := 1; i < len(content.TextCards); i++ {
		logrus.Infof("text_to_image: 添加第 %d 张卡片", i+1)
		if err := clickByText(page, "再写一张"); err != nil {
			return errors.Wrapf(err, "点击再写一张失败")
		}
		humanSleep(500 * time.Millisecond)
		if err := inputLastCard(page, content.TextCards[i]); err != nil {
			return errors.Wrapf(err, "输入第 %d 张卡片失败", i+1)
		}
	}

	// 4. 点击"生成图片"
	logrus.Info("text_to_image: 点击生成图片")
	if err := clickByText(page, "生成图片"); err != nil {
		return errors.Wrap(err, "点击生成图片失败")
	}

	// 等待预览页面加载（生成图片需要渲染时间）
	time.Sleep(5 * time.Second)
	if err := page.WaitDOMStable(2*time.Second, 0.1); err != nil {
		logrus.Warnf("等待预览页 DOM 稳定: %v", err)
	}

	// 5. 选择样式（默认基础，不需要额外点击）
	if content.ImageStyle != "" && content.ImageStyle != "基础" {
		logrus.Infof("text_to_image: 选择样式: %s", content.ImageStyle)
		if err := clickByText(page, content.ImageStyle); err != nil {
			logrus.Warnf("选择样式失败，使用默认: %v", err)
		}
		time.Sleep(2 * time.Second)
	}

	// 调试：列出页面上所有按钮文本
	btnTexts, _ := page.Eval(`() => {
		const btns = document.querySelectorAll('button, [role="button"]');
		const texts = [];
		btns.forEach(b => {
			if (b.offsetParent !== null) texts.push(b.textContent.trim().substring(0, 50));
		});
		return texts.join(' | ');
	}`)
	if btnTexts != nil {
		logrus.Infof("text_to_image: 页面按钮列表: %s", btnTexts.Value.String())
	}

	// 调试：截图保存
	_ = page.MustScreenshotFullPage("/tmp/xhs-text-image-preview.png")

	// 6. 点击"下一步"按钮（红色按钮，在页面底部）
	logrus.Info("text_to_image: 点击下一步")
	if err := clickNextStepButton(page); err != nil {
		return errors.Wrap(err, "点击下一步失败")
	}
	humanSleep(2 * time.Second)

	// 7. 等待发布编辑页加载
	logrus.Info("text_to_image: 等待发布编辑页加载")
	if err := page.WaitLoad(); err != nil {
		logrus.Warnf("等待发布页加载: %v", err)
	}
	humanSleep(3 * time.Second)
	if err := page.WaitDOMStable(2*time.Second, 0.1); err != nil {
		logrus.Warnf("等待发布页 DOM 稳定: %v", err)
	}

	// 8. 在发布编辑页填写标题、正文、标签并发布
	logrus.Info("text_to_image: 填写发布信息并提交")
	return textImageSubmitPublish(page, content)
}

// clickByText 通过文本匹配点击元素，优先 button > span > a > div
func clickByText(page *rod.Page, text string) error {
	tagPriority := []string{"button", "span", "a", "div"}
	for _, tag := range tagPriority {
		el, err := page.Timeout(3*time.Second).ElementR(tag, "^"+text+"$")
		if err == nil && el != nil {
			if visible, _ := el.Visible(); visible {
				logrus.Infof("clickByText: 精确匹配 '%s', tag=%s", text, tag)
				el.MustScrollIntoView()
				humanSleep(300 * time.Millisecond)
				return humanClick(page, el)
			}
		}
	}
	// 备用：模糊匹配
	for _, tag := range tagPriority {
		el, err := page.Timeout(3*time.Second).ElementR(tag, text)
		if err == nil && el != nil {
			if visible, _ := el.Visible(); visible {
				logrus.Infof("clickByText: 模糊匹配 '%s', tag=%s", text, tag)
				el.MustScrollIntoView()
				humanSleep(300 * time.Millisecond)
				return humanClick(page, el)
			}
		}
	}
	return errors.Errorf("未找到文本: %s", text)
}

// inputFirstCard 在第一个卡片编辑区输入文字
func inputFirstCard(page *rod.Page, text string) error {
	el, err := page.Timeout(5 * time.Second).Element(`div[contenteditable="true"]`)
	if err != nil {
		return errors.Wrap(err, "未找到卡片输入区")
	}
	if err := humanClick(page, el); err != nil {
		return err
	}
	humanSleep(300 * time.Millisecond)
	return el.Input(text)
}

// clickNextStepButton 点击"下一步"按钮（预览页底部的红色按钮）
func clickNextStepButton(page *rod.Page) error {
	// 尝试多种选择器
	selectors := []string{
		`button.css-k01y3m`,        // 红色主按钮
		`button.publishBtn`,        // 发布类按钮
		`button[class*="primary"]`, // primary 类型按钮
	}

	for _, sel := range selectors {
		el, err := page.Timeout(3 * time.Second).Element(sel)
		if err == nil && el != nil {
			text, _ := el.Text()
			if strings.Contains(text, "下一步") {
				logrus.Infof("text_to_image: 找到下一步按钮 via %s", sel)
				return humanClick(page, el)
			}
		}
	}

	// 备用：通过文本查找所有 button
	return clickByText(page, "下一步")
}

// textImageSubmitPublish 文字配图模式的发布页提交（DOM 与普通图文不同）
func textImageSubmitPublish(page *rod.Page, content PublishTextImageContent) error {
	// 输入标题（placeholder "填写标题会有更多赞哦"）
	logrus.Infof("text_to_image: 输入标题: %s", content.Title)
	titleSelectors := []string{
		`input[placeholder*="标题"]`,
		`div.d-input input`,
		`input[maxlength]`,
	}
	for _, sel := range titleSelectors {
		el, err := page.Timeout(5 * time.Second).Element(sel)
		if err == nil && el != nil {
			if err := humanClick(page, el); err != nil {
				logrus.Warnf("点击标题失败: %v", err)
			}
			if err := el.SelectAllText(); err == nil {
				_ = el.Input(content.Title)
				logrus.Infof("text_to_image: 标题输入成功 via %s", sel)
				break
			}
		}
	}
	humanSleep(500 * time.Millisecond)

	// 输入正文（图下正文，替换默认填充的卡片文字）
	if content.Content != "" {
		logrus.Info("text_to_image: 输入正文")
		contentSelectors := []string{
			`div.ql-editor`,
			`div[contenteditable="true"][role="textbox"]`,
			`div[contenteditable="true"]`,
		}
		for _, sel := range contentSelectors {
			els, err := page.Elements(sel)
			if err != nil || len(els) == 0 {
				continue
			}
			// 取最后一个（标题可能也是 contenteditable）
			el := els[len(els)-1]
			if visible, _ := el.Visible(); !visible {
				continue
			}
			// 清空后输入。这里用原生 elem.Click 而不是 humanClick——
			// 实测 humanClick 的坐标抖动 (bbox 中心 30-70%) 在 Quill 编辑器上
			// 会落到不同的 paragraph div，导致后续 el.Input 里的 \n 换行符
			// 被编辑器吞掉（发布后段落粘在一起）。editor 正文的点击必须命中
			// 固定的编辑区，走 elem.Click（命中元素中心）稳定。
			if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
				continue
			}
			humanSleep(200 * time.Millisecond)
			// 先用 JS 清空 Quill 编辑器内容——text-to-image 模式下
			// 点击"下一步"后编辑器会预填卡片文字（即标题），
			// SelectAllText()+Input() 无法可靠清除，导致正文前面多出标题。
			el.Eval(`function() { this.innerHTML = '<p><br></p>' }`)
			humanSleep(100 * time.Millisecond)
			_ = el.SelectAllText()
			_ = el.Input(content.Content)
			logrus.Infof("text_to_image: 正文输入成功 via %s", sel)
			break
		}
	}
	humanSleep(500 * time.Millisecond)

	// 输入标签（在正文区域用 #标签 方式）
	if len(content.Tags) > 0 {
		logrus.Infof("text_to_image: 输入标签: %v", content.Tags)
		tagSelectors := []string{
			`div[contenteditable="true"][role="textbox"]`,
			`div.ql-editor`,
			`div[contenteditable="true"]`,
		}
		for _, sel := range tagSelectors {
			els, err := page.Elements(sel)
			if err != nil || len(els) == 0 {
				continue
			}
			el := els[len(els)-1]
			if visible, _ := el.Visible(); !visible {
				continue
			}
			// 点击正文区聚焦，Ctrl+End 移到末尾，再回车换行
			// 原因同上：editor 内用原生 elem.Click 保证命中同一 paragraph div
			_ = el.Click(proto.InputMouseButtonLeft, 1)
			humanSleep(300 * time.Millisecond)
			_, _ = page.Eval(`() => {
				const sel = window.getSelection();
				const el = document.querySelector('[contenteditable="true"][role="textbox"]') || document.querySelector('[contenteditable="true"]');
				if (el) {
					const range = document.createRange();
					range.selectNodeContents(el);
					range.collapse(false);
					sel.removeAllRanges();
					sel.addRange(range);
				}
			}`)
			humanSleep(200 * time.Millisecond)
			// 回车换行
			ka, _ := el.KeyActions()
			if ka != nil {
				_ = ka.Press(input.Enter).Do()
			}
			humanSleep(300 * time.Millisecond)

			// 逐个输入标签
			for _, tag := range content.Tags {
				tag = strings.TrimLeft(tag, "#")
				if err := inputTag(el, tag); err != nil {
					logrus.Warnf("输入标签 [%s] 失败: %v", tag, err)
				}
			}
			logrus.Info("text_to_image: 标签输入完成")
			break
		}
	}

	// 设置可见范围
	if content.Visibility != "" && content.Visibility != "公开可见" {
		if err := setVisibility(page, content.Visibility); err != nil {
			logrus.Warnf("设置可见范围失败: %v", err)
		}
	}

	// 设置定时发布
	if content.ScheduleTime != nil {
		if err := setSchedulePublish(page, *content.ScheduleTime); err != nil {
			logrus.Warnf("设置定时发布失败: %v", err)
		}
	}

	// 点击发布
	logrus.Info("text_to_image: 点击发布按钮")
	// "review" pause before final publish click
	humanSleep(1200 * time.Millisecond)
	if err := clickByText(page, "发布"); err != nil {
		return errors.Wrap(err, "点击发布按钮失败")
	}

	// 等待发布结果
	time.Sleep(5 * time.Second)
	_ = page.MustScreenshotFullPage("/tmp/xhs-text-image-result.png")

	// 检查是否有错误提示
	info, _ := page.Info()
	logrus.Infof("text_to_image: 发布后页面 URL: %s", info.URL)

	return nil
}

// inputLastCard 在最后一个（新添加的）卡片编辑区输入文字
func inputLastCard(page *rod.Page, text string) error {
	humanSleep(500 * time.Millisecond)
	els, err := page.Elements(`div[contenteditable="true"]`)
	if err != nil || len(els) == 0 {
		return errors.New("未找到卡片输入区")
	}
	lastEl := els[len(els)-1]
	if err := humanClick(page, lastEl); err != nil {
		return err
	}
	humanSleep(300 * time.Millisecond)
	return lastEl.Input(text)
}
