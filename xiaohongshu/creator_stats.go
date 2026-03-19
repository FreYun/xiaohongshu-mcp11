package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

const urlCreatorHome = "https://creator.xiaohongshu.com/new/home?source=official"

// CreatorProfile 创作者个人基本信息
type CreatorProfile struct {
	Nickname      string `json:"nickname"`
	AccountID     string `json:"accountId"`     // 小红书账号号码
	AccountStatus string `json:"accountStatus"` // 账号状态，如"正常"
	Follows       string `json:"follows"`       // 关注数
	Fans          string `json:"fans"`          // 粉丝数
	Interactions  string `json:"interactions"`  // 获赞与收藏
}

// NoteStatItem 单个统计项
type NoteStatItem struct {
	Name   string `json:"name"`   // 指标名，如"曝光数"
	Value  string `json:"value"`  // 数值
	Change string `json:"change"` // 环比，如"+24%"
}

// CreatorStats 近7天笔记数据总览
type CreatorStats struct {
	Period string         `json:"period"` // 统计周期，如"03-04 至 03-10"
	Items  []NoteStatItem `json:"items"`
}

// CreatorHomeInfo 创作者首页完整信息
type CreatorHomeInfo struct {
	Profile CreatorProfile `json:"profile"`
	Stats   CreatorStats   `json:"stats"`
}

// CreatorStatsAction 创作者首页数据抓取
type CreatorStatsAction struct {
	page *rod.Page
}

func NewCreatorStatsAction(page *rod.Page) *CreatorStatsAction {
	return &CreatorStatsAction{page: page.Timeout(60 * time.Second)}
}

// GetCreatorHome 获取创作者首页信息（个人资料 + 近7天数据总览）
func (a *CreatorStatsAction) GetCreatorHome(ctx context.Context) (*CreatorHomeInfo, error) {
	page := a.page.Context(ctx)

	// 先进发布页建立创作者平台 session（通过主站 web_session 自动授权，无需二次登录）
	page.MustNavigate("https://creator.xiaohongshu.com/publish/publish?source=official").MustWaitLoad()
	humanSleep(2 * time.Second)

	// 再跳创作者首页，等待账号昵称元素渲染完成
	page.MustNavigate(urlCreatorHome).MustWaitLoad()
	for i := 0; i < 15; i++ {
		humanSleep(1 * time.Second)
		res, _ := page.Eval(`() => { var el = document.querySelector('.account-name'); return el ? el.textContent.trim() : ''; }`)
		if res != nil && res.Value.Str() != "" {
			break
		}
	}

	// 检测是否仍在登录页（理论上不会，但兜底保护）
	currentURL := page.MustInfo().URL
	if strings.Contains(currentURL, "/login") {
		return nil, fmt.Errorf("创作者平台未登录（%s），主站 web_session 可能已失效", currentURL)
	}

	result, err := page.Eval(`() => {
		const info = {
			profile: { nickname: '', accountId: '', accountStatus: '', follows: '', fans: '', interactions: '' },
			stats: { period: '', items: [] }
		};

		const allText = document.body.innerText;

		// ── 个人信息 ──────────────────────────────────────────

		// 用户名：.account-name
		const nameEl = document.querySelector('.account-name') || document.querySelector('.name-box');
		if (nameEl) info.profile.nickname = nameEl.textContent.trim();

		// 账号状态
		const statusMatch = allText.match(/账号状态\s*(.{1,6})/);
		if (statusMatch) info.profile.accountStatus = statusMatch[1].trim();

		// 小红书账号号码
		const idMatch = allText.match(/小红书账号[：:]\s*(\d+)/);
		if (idMatch) info.profile.accountId = idMatch[1];

		// 关注/粉丝/获赞：数字出现在标签前一行，用正文正则提取
		const followMatch = allText.match(/(\S+)\n关注数/);
		if (followMatch) info.profile.follows = followMatch[1].trim();
		const fansMatch = allText.match(/(\S+)\n粉丝数/);
		if (fansMatch) info.profile.fans = fansMatch[1].trim();
		const interMatch = allText.match(/(\S+)\n获赞与收藏/);
		if (interMatch) info.profile.interactions = interMatch[1].trim();

		// ── 数据总览 ──────────────────────────────────────────

		// 统计周期：从正文提取 "03-04 至 03-10"
		const periodMatch = allText.match(/(\d{2}-\d{2}\s*至\s*\d{2}-\d{2})/);
		if (periodMatch) info.stats.period = periodMatch[1].trim();

		// 数据卡片：选 .creator-block.default-cursor，排除 .grouped-note-data-grid 内的占位块（值为0）
		// 结构：.creator-block > .title(指标名) + .number-container > span.number(数值) + .tendency > span.tendency-number(环比)
		const allCards = Array.from(document.querySelectorAll('.creator-block.default-cursor'));
		allCards.filter(card => !card.closest('.grouped-note-data-grid')).forEach(card => {
			const titleEl = card.querySelector('.title');
			const numEl = card.querySelector('.number-container .number, .number-container span, .numerical');
			const changeEls = card.querySelectorAll('.tendency-number');
			if (!titleEl || !numEl) return;
			const name = titleEl.textContent.trim();
			const value = numEl.textContent.trim();
			if (!name || value === '') return;
			// tendency-number 可能有多个（标签"环比" + 实际数值），取最后一个非"环比"文本
			let change = '';
			changeEls.forEach(el => {
				const t = el.textContent.trim();
				if (t && t !== '环比') change = t;
			});
			info.stats.items.push({ name, value, change });
		});

		return JSON.stringify(info);
	}`)
	if err != nil {
		return nil, fmt.Errorf("提取创作者首页数据失败: %w", err)
	}

	raw := result.Value.Str()
	if raw == "" {
		return nil, fmt.Errorf("页面返回空数据，可能未登录或页面结构已变更")
	}

	var info CreatorHomeInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return nil, fmt.Errorf("解析数据失败: %w", err)
	}

	return &info, nil
}
