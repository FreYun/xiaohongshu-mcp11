package xiaohongshu

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-rod/rod"
)

const (
	urlOfDataAnalysis = "https://creator.xiaohongshu.com/statistics/data-analysis?source=official"
)

// NotePerformance 笔记内容分析数据（创作者中心 - 数据看板 - 内容分析）
type NotePerformance struct {
	Title        string `json:"title"`         // 标题
	PublishTime  string `json:"publish_time"`  // 发布时间
	Impressions  string `json:"impressions"`   // 曝光
	Views        string `json:"views"`         // 观看
	ClickRate    string `json:"click_rate"`    // 封面点击率
	Likes        string `json:"likes"`         // 点赞
	Comments     string `json:"comments"`      // 评论
	Favorites    string `json:"favorites"`     // 收藏
	NewFollowers string `json:"new_followers"` // 涨粉
	Shares       string `json:"shares"`        // 分享
	AvgWatchTime string `json:"avg_watch_time"` // 人均观看时长
	Danmaku      string `json:"danmaku"`       // 弹幕
}

// NotesPerformanceAction 内容分析页面操作
type NotesPerformanceAction struct {
	page *rod.Page
}

// NewNotesPerformanceAction 创建内容分析操作
func NewNotesPerformanceAction(page *rod.Page) *NotesPerformanceAction {
	return &NotesPerformanceAction{page: page}
}

// GetNotesPerformance 获取内容分析页面的笔记数据
func (a *NotesPerformanceAction) GetNotesPerformance(ctx context.Context) ([]NotePerformance, error) {
	a.page = a.page.Context(ctx).Timeout(60 * time.Second)

	slog.Info("导航到内容分析页面", "url", urlOfDataAnalysis)
	if err := a.page.Navigate(urlOfDataAnalysis); err != nil {
		return nil, fmt.Errorf("导航到内容分析页面失败: %w", err)
	}
	if err := a.page.WaitLoad(); err != nil {
		slog.Warn("等待页面加载", "error", err)
	}
	humanSleep(3 * time.Second)
	if err := a.page.WaitDOMStable(time.Second, 0.1); err != nil {
		slog.Warn("等待 DOM 稳定", "error", err)
	}
	humanSleep(2 * time.Second)

	if err := checkCreatorPageLogin(a.page); err != nil {
		return nil, err
	}

	result, err := a.page.Eval(`() => {
		const notes = [];
		// 内容分析表格的每一行
		const rows = document.querySelectorAll('table tbody tr');

		rows.forEach(row => {
			const cells = row.querySelectorAll('td');
			if (cells.length < 5) return;

			// 第一列：笔记基础信息（包含标题和发布时间）
			const infoCell = cells[0];
			const fullText = (infoCell.textContent || '').trim();

			// 分离标题和发布时间：时间格式为 "发布于YYYY-MM-DD HH:MM" 或 "YYYY-MM-DD HH:MM"
			let title = '', publishTime = '';
			const timeMatch = fullText.match(/(发布于)?(\d{4}-\d{2}-\d{2}\s*\d{2}:\d{2})/);
			if (timeMatch) {
				publishTime = timeMatch[2].trim();
				title = fullText.substring(0, fullText.indexOf(timeMatch[0])).trim();
			} else {
				title = fullText.substring(0, 50);
			}

			// 剩余列：从第 1 列开始按顺序取数值
			// 顺序：曝光、观看、封面点击率、点赞、评论、收藏、涨粉、分享、人均观看时长、弹幕
			const metrics = [];
			for (let i = 1; i < cells.length; i++) {
				// 取 cell 里最深层的纯数字/百分比文本，跳过排序箭头等装饰
				let val = '';
				const spans = cells[i].querySelectorAll('span, div, em');
				if (spans.length > 0) {
					for (const s of spans) {
						const t = (s.textContent || '').trim();
						if (t && /^[\d,.]+%?$/.test(t) || t === '-' || /^\d+[smh]$/.test(t)) {
							val = t;
							break;
						}
					}
				}
				if (!val) val = (cells[i].textContent || '').trim();
				metrics.push(val);
			}

			if (title || publishTime) {
				notes.push({
					title: title,
					publish_time: publishTime,
					impressions:    metrics[0] || '-',
					views:          metrics[1] || '-',
					click_rate:     metrics[2] || '-',
					likes:          metrics[3] || '-',
					comments:       metrics[4] || '-',
					favorites:      metrics[5] || '-',
					new_followers:  metrics[6] || '-',
					shares:         metrics[7] || '-',
					avg_watch_time: metrics[8] || '-',
					danmaku:        metrics[9] || '-',
				});
			}
		});

		return notes;
	}`)
	if err != nil {
		return nil, fmt.Errorf("获取内容分析数据失败: %w", err)
	}

	var notes []NotePerformance
	for _, v := range result.Value.Arr() {
		notes = append(notes, NotePerformance{
			Title:        v.Get("title").String(),
			PublishTime:  v.Get("publish_time").String(),
			Impressions:  v.Get("impressions").String(),
			Views:        v.Get("views").String(),
			ClickRate:    v.Get("click_rate").String(),
			Likes:        v.Get("likes").String(),
			Comments:     v.Get("comments").String(),
			Favorites:    v.Get("favorites").String(),
			NewFollowers: v.Get("new_followers").String(),
			Shares:       v.Get("shares").String(),
			AvgWatchTime: v.Get("avg_watch_time").String(),
			Danmaku:      v.Get("danmaku").String(),
		})
	}

	return notes, nil
}
