package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/errors"
)

type SearchResult struct {
	Search struct {
		Feeds FeedsValue `json:"feeds"`
	} `json:"search"`
}

// FilterOption 筛选选项结构体
type FilterOption struct {
	SortBy      string `json:"sort_by,omitempty" jsonschema:"排序依据: 综合|最新|最多点赞|最多评论|最多收藏,默认为'综合'"`
	NoteType    string `json:"note_type,omitempty" jsonschema:"笔记类型: 不限|视频|图文,默认为'不限'"`
	PublishTime string `json:"publish_time,omitempty" jsonschema:"发布时间: 不限|一天内|一周内|半年内,默认为'不限'"`
	SearchScope string `json:"search_scope,omitempty" jsonschema:"搜索范围: 不限|已看过|未看过|已关注,默认为'不限'"`
	Location    string `json:"location,omitempty" jsonschema:"位置距离: 不限|同城|附近,默认为'不限'"`
}

// internalFilterOption 内部使用的筛选选项(基于索引)
type internalFilterOption struct {
	FiltersIndex int    // 筛选组索引
	TagsIndex    int    // 标签索引
	Text         string // 标签文本描述
}

// 预定义的筛选选项映射表（内部使用）
var filterOptionsMap = map[int][]internalFilterOption{
	1: { // 排序依据
		{FiltersIndex: 1, TagsIndex: 1, Text: "综合"},
		{FiltersIndex: 1, TagsIndex: 2, Text: "最新"},
		{FiltersIndex: 1, TagsIndex: 3, Text: "最多点赞"},
		{FiltersIndex: 1, TagsIndex: 4, Text: "最多评论"},
		{FiltersIndex: 1, TagsIndex: 5, Text: "最多收藏"},
	},
	2: { // 笔记类型
		{FiltersIndex: 2, TagsIndex: 1, Text: "不限"},
		{FiltersIndex: 2, TagsIndex: 2, Text: "视频"},
		{FiltersIndex: 2, TagsIndex: 3, Text: "图文"},
	},
	3: { // 发布时间
		{FiltersIndex: 3, TagsIndex: 1, Text: "不限"},
		{FiltersIndex: 3, TagsIndex: 2, Text: "一天内"},
		{FiltersIndex: 3, TagsIndex: 3, Text: "一周内"},
		{FiltersIndex: 3, TagsIndex: 4, Text: "半年内"},
	},
	4: { // 搜索范围
		{FiltersIndex: 4, TagsIndex: 1, Text: "不限"},
		{FiltersIndex: 4, TagsIndex: 2, Text: "已看过"},
		{FiltersIndex: 4, TagsIndex: 3, Text: "未看过"},
		{FiltersIndex: 4, TagsIndex: 4, Text: "已关注"},
	},
	5: { // 位置距离
		{FiltersIndex: 5, TagsIndex: 1, Text: "不限"},
		{FiltersIndex: 5, TagsIndex: 2, Text: "同城"},
		{FiltersIndex: 5, TagsIndex: 3, Text: "附近"},
	},
}

// convertToInternalFilters 将 FilterOption 转换为内部的 internalFilterOption 列表
func convertToInternalFilters(filter FilterOption) ([]internalFilterOption, error) {
	var internalFilters []internalFilterOption

	// 处理排序依据
	if filter.SortBy != "" {
		internal, err := findInternalOption(1, filter.SortBy)
		if err != nil {
			return nil, fmt.Errorf("排序依据错误: %w", err)
		}
		internalFilters = append(internalFilters, internal)
	}

	// 处理笔记类型
	if filter.NoteType != "" {
		internal, err := findInternalOption(2, filter.NoteType)
		if err != nil {
			return nil, fmt.Errorf("笔记类型错误: %w", err)
		}
		internalFilters = append(internalFilters, internal)
	}

	// 处理发布时间
	if filter.PublishTime != "" {
		internal, err := findInternalOption(3, filter.PublishTime)
		if err != nil {
			return nil, fmt.Errorf("发布时间错误: %w", err)
		}
		internalFilters = append(internalFilters, internal)
	}

	// 处理搜索范围
	if filter.SearchScope != "" {
		internal, err := findInternalOption(4, filter.SearchScope)
		if err != nil {
			return nil, fmt.Errorf("搜索范围错误: %w", err)
		}
		internalFilters = append(internalFilters, internal)
	}

	// 处理位置距离
	if filter.Location != "" {
		internal, err := findInternalOption(5, filter.Location)
		if err != nil {
			return nil, fmt.Errorf("位置距离错误: %w", err)
		}
		internalFilters = append(internalFilters, internal)
	}

	return internalFilters, nil
}

// findInternalOption 根据筛选组索引和文本查找内部筛选选项
func findInternalOption(filtersIndex int, text string) (internalFilterOption, error) {
	options, exists := filterOptionsMap[filtersIndex]
	if !exists {
		return internalFilterOption{}, fmt.Errorf("筛选组 %d 不存在", filtersIndex)
	}

	for _, option := range options {
		if option.Text == text {
			return option, nil
		}
	}

	return internalFilterOption{}, fmt.Errorf("在筛选组 %d 中未找到文本 '%s'", filtersIndex, text)
}

// validateInternalFilterOption 验证内部筛选选项是否在有效范围内
func validateInternalFilterOption(filter internalFilterOption) error {
	// 检查筛选组索引是否有效
	if filter.FiltersIndex < 1 || filter.FiltersIndex > 5 {
		return fmt.Errorf("无效的筛选组索引 %d，有效范围为 1-5", filter.FiltersIndex)
	}

	// 检查标签索引是否在对应筛选组的有效范围内
	options, exists := filterOptionsMap[filter.FiltersIndex]
	if !exists {
		return fmt.Errorf("筛选组 %d 不存在", filter.FiltersIndex)
	}

	if filter.TagsIndex < 1 || filter.TagsIndex > len(options) {
		return fmt.Errorf("筛选组 %d 的标签索引 %d 超出范围，有效范围为 1-%d",
			filter.FiltersIndex, filter.TagsIndex, len(options))
	}

	return nil
}

type SearchAction struct {
	page *rod.Page
}

func NewSearchAction(page *rod.Page) *SearchAction {
	pp := page.Timeout(60 * time.Second)

	return &SearchAction{page: pp}
}

func (s *SearchAction) Search(ctx context.Context, keyword string, filters ...FilterOption) ([]Feed, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	page := s.page.Context(ctx)

	// 模拟真实用户：打开 explore → 点搜索框 → 逐字输入 → 回车
	if err := humanSearch(page, keyword); err != nil {
		logrus.Warnf("humanSearch failed, fallback to direct URL: %v", err)
		searchURL := makeSearchURL(keyword)
		page.MustNavigate(searchURL).MustWaitLoad()
	}

	page.MustWait(`() => window.__INITIAL_STATE__ !== undefined`)
	humanSleepRange(1500, 3500)

	// 如果有筛选条件，则应用筛选
	if len(filters) > 0 {
		// 将所有 FilterOption 转换为内部筛选选项
		var allInternalFilters []internalFilterOption
		for _, filter := range filters {
			internalFilters, err := convertToInternalFilters(filter)
			if err != nil {
				return nil, fmt.Errorf("筛选选项转换失败: %w", err)
			}
			allInternalFilters = append(allInternalFilters, internalFilters...)
		}

		// 验证所有内部筛选选项
		for _, filter := range allInternalFilters {
			if err := validateInternalFilterOption(filter); err != nil {
				return nil, fmt.Errorf("筛选选项验证失败: %w", err)
			}
		}

		// 悬停在筛选按钮上（保留 MustHover 触发面板，叠加随机停顿）
		filterButton := page.MustElement(`div.filter`)
		filterButton.MustHover()
		humanSleepRange(400, 900)

		// 等待筛选面板出现
		page.MustWait(`() => document.querySelector('div.filter-panel') !== null`)

		// 应用所有筛选条件，用 humanClick 替代 MustClick，条件间插入短停顿
		for i, filter := range allInternalFilters {
			selector := fmt.Sprintf(`div.filter-panel div.filters:nth-child(%d) div.tags:nth-child(%d)`,
				filter.FiltersIndex, filter.TagsIndex)
			option := page.MustElement(selector)
			if err := humanClick(page, option); err != nil {
				logrus.Warnf("humanClick filter option failed, fallback: %v", err)
				option.MustClick()
			}
			if i < len(allInternalFilters)-1 {
				humanSleepRange(350, 800)
			}
		}

		// 等待筛选后页面更新
		humanSleepRange(1500, 2800)
		page.MustWait(`() => window.__INITIAL_STATE__ !== undefined`)
	}

	result := page.MustEval(`() => {
		if (window.__INITIAL_STATE__ &&
		    window.__INITIAL_STATE__.search &&
		    window.__INITIAL_STATE__.search.feeds) {
			const feeds = window.__INITIAL_STATE__.search.feeds;
			const feedsData = feeds.value !== undefined ? feeds.value : feeds._value;
			if (feedsData) {
				return JSON.stringify(feedsData);
			}
		}
		return "";
	}`).String()

	if result == "" {
		return nil, errors.ErrNoFeeds
	}

	var feeds []Feed
	if err := json.Unmarshal([]byte(result), &feeds); err != nil {
		return nil, fmt.Errorf("failed to unmarshal feeds: %w", err)
	}

	return feeds, nil
}

// humanSearch 模拟真实用户在 explore 页面搜索的完整路径：
// 打开 explore → 浏览 → 点击搜索框 → 逐字输入关键词 → 按回车 → 等待结果。
func humanSearch(page *rod.Page, keyword string) error {
	logrus.Info("humanSearch: 导航到 explore 首页")
	if err := page.Navigate("https://www.xiaohongshu.com/explore"); err != nil {
		return fmt.Errorf("navigate explore: %w", err)
	}
	if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("explore WaitDOMStable: %v", err)
	}
	humanSleepRange(1500, 3000)

	// 点击搜索框（explore 页顶部的搜索输入框）
	searchInput, err := page.Timeout(10 * time.Second).Element(`#search-input`)
	if err != nil {
		return fmt.Errorf("find search input: %w", err)
	}

	if err := humanClick(page, searchInput); err != nil {
		return fmt.Errorf("click search input: %w", err)
	}
	humanSleepRange(300, 700)

	// 逐字符输入关键词
	logrus.Infof("humanSearch: 输入关键词 '%s'", keyword)
	if err := humanType(searchInput, keyword); err != nil {
		return fmt.Errorf("type keyword: %w", err)
	}
	humanSleepRange(500, 1200)

	// 按回车搜索
	if err := page.Keyboard.Press(input.Enter); err != nil {
		return fmt.Errorf("press enter: %w", err)
	}

	// 等待搜索结果页加载
	if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("search result WaitDOMStable: %v", err)
	}

	logrus.Info("humanSearch: 搜索结果页已加载")
	return nil
}

// ensureOnExplore makes sure the page is already sitting on a xiaohongshu.com
// URL before we navigate to the search-result page. This sets a proper
// Referer on the search request (instead of about:blank / empty), which
// matches the normal "visit homepage → search" flow real users produce.
// No-op if we're already on the site.
func ensureOnExplore(page *rod.Page) {
	info, err := page.Info()
	if err == nil && info != nil && strings.Contains(info.URL, "xiaohongshu.com") {
		return
	}
	logrus.Debug("search: not on xiaohongshu.com, bouncing through /explore first")
	if err := page.Navigate("https://www.xiaohongshu.com/explore"); err != nil {
		logrus.Warnf("search: pre-navigate explore failed: %v", err)
		return
	}
	if err := page.WaitLoad(); err != nil {
		logrus.Warnf("search: explore WaitLoad failed: %v", err)
	}
	humanSleepRange(1200, 2500)
}

func makeSearchURL(keyword string) string {

	values := url.Values{}
	values.Set("keyword", keyword)
	values.Set("source", "web_explore_feed")

	//https://www.xiaohongshu.com/search_result?keyword=%25E7%258E%258B%25E5%25AD%2590&source=web_search_result_notes
	//https://www.xiaohongshu.com/search_result?keyword=%25E7%258E%258B%25E5%25AD%2590&source=web_explore_feed
	return fmt.Sprintf("https://www.xiaohongshu.com/search_result?%s", values.Encode())
}
