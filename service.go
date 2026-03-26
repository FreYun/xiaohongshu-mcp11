package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/browser"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/pkg/downloader"
	"github.com/xpzouying/xiaohongshu-mcp/pkg/xhsutil"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

// XiaohongshuService 小红书业务服务
type XiaohongshuService struct{}

// NewXiaohongshuService 创建小红书服务实例
func NewXiaohongshuService() *XiaohongshuService {
	return &XiaohongshuService{}
}

// PublishRequest 发布请求
type PublishRequest struct {
	Title      string   `json:"title" binding:"required"`
	Content    string   `json:"content" binding:"required"`
	Images     []string `json:"images" binding:"required,min=1"`
	Tags       []string `json:"tags,omitempty"`
	ScheduleAt string   `json:"schedule_at,omitempty"` // 定时发布时间，ISO8601格式，为空则立即发布
	IsOriginal bool     `json:"is_original,omitempty"` // 是否声明原创
	Visibility string   `json:"visibility,omitempty"`  // 可见范围: "公开可见"(默认), "仅自己可见", "仅互关好友可见"
	Products   []string `json:"products,omitempty"`    // 商品关键词列表，用于绑定带货商品
}

// LoginStatusResponse 登录状态响应
type LoginStatusResponse struct {
	IsLoggedIn bool   `json:"is_logged_in"`
	Username   string `json:"username,omitempty"`
}

// LoginQrcodeResponse 登录扫码二维码
type LoginQrcodeResponse struct {
	Timeout    string `json:"timeout"`
	IsLoggedIn bool   `json:"is_logged_in"`
	Img        string `json:"img,omitempty"`
}

// PublishResponse 发布响应
type PublishResponse struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Images  int    `json:"images"`
	Status  string `json:"status"`
	PostID  string `json:"post_id,omitempty"`
}

// PublishVideoRequest 发布视频请求（仅支持本地单个视频文件）
type PublishVideoRequest struct {
	Title      string   `json:"title" binding:"required"`
	Content    string   `json:"content" binding:"required"`
	Video      string   `json:"video" binding:"required"`
	Tags       []string `json:"tags,omitempty"`
	ScheduleAt string   `json:"schedule_at,omitempty"` // 定时发布时间，ISO8601格式，为空则立即发布
	Visibility string   `json:"visibility,omitempty"`  // 可见范围: "公开可见"(默认), "仅自己可见", "仅互关好友可见"
	Products   []string `json:"products,omitempty"`    // 商品关键词列表，用于绑定带货商品
}

// PublishVideoResponse 发布视频响应
type PublishVideoResponse struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Video   string `json:"video"`
	Status  string `json:"status"`
	PostID  string `json:"post_id,omitempty"`
}

// FeedsListResponse Feeds列表响应
type FeedsListResponse struct {
	Feeds []xiaohongshu.Feed `json:"feeds"`
	Count int                `json:"count"`
}

// UserProfileResponse 用户主页响应
type UserProfileResponse struct {
	UserBasicInfo xiaohongshu.UserBasicInfo      `json:"userBasicInfo"`
	Interactions  []xiaohongshu.UserInteractions `json:"interactions"`
	Feeds         []xiaohongshu.Feed             `json:"feeds"`
}

// DeleteCookies 全量清除登录状态：删除 cookies.json + 清除浏览器内部 cookie。
// 这确保 cookie 模式和 profile 模式下主站、创作者平台的 cookie 都被清除。
func (s *XiaohongshuService) DeleteCookies(ctx context.Context, botID string) error {
	// 1. 删除 cookies.json 文件
	cookiePath := cookies.GetCookiesFilePathForBot(botID)
	cookieLoader := cookies.NewLoadCookie(cookiePath)
	fileErr := cookieLoader.DeleteCookies()

	// 2. 启动浏览器并清除内部 cookie（profile 模式下 Chrome 会持久化 cookie）
	b := newBrowserForBot(botID)
	page := b.NewPage()
	browserErr := page.Browser().SetCookies(nil)
	_ = page.Close()
	b.Close()

	if browserErr != nil {
		logrus.Warnf("清除浏览器内部 cookie 失败: %v", browserErr)
	} else {
		logrus.Info("已清除浏览器内部 cookie")
	}

	return fileErr
}

// CheckLoginStatus 检查登录状态
func (s *XiaohongshuService) CheckLoginStatus(ctx context.Context, botID string) (*LoginStatusResponse, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	loginAction := xiaohongshu.NewLogin(page)

	isLoggedIn, err := loginAction.CheckLoginStatus(ctx)
	if err != nil {
		return nil, err
	}

	response := &LoginStatusResponse{
		IsLoggedIn: isLoggedIn,
		Username:   configs.Username,
	}

	return response, nil
}

// GetLoginQrcode 获取登录的扫码二维码。
// notifySession：可选，扫码成功保存 cookie 后通过 openclaw 向该 session 回传通知。
func (s *XiaohongshuService) GetLoginQrcode(ctx context.Context, botID string, notifySession string) (*LoginQrcodeResponse, error) {
	b := newBrowserForBot(botID)
	page := b.NewPage()

	deferFunc := func() {
		_ = page.Close()
		b.Close()
	}

	// 清除浏览器内已有 cookie（尤其是旧 web_session），确保以全新 session 生成 QR。
	// 如果浏览器带着旧 session 生成 QR，扫码时 XHS 会因 session 冲突而报 "fail to login"。
	if err := page.Browser().SetCookies(nil); err != nil {
		logrus.Warnf("main login: 清除 cookies 失败: %v", err)
	} else {
		logrus.Info("main login: 已清除浏览器 cookies")
	}

	loginAction := xiaohongshu.NewLogin(page)

	img, loggedIn, err := loginAction.FetchQrcodeImage(ctx)
	if err != nil || loggedIn {
		defer deferFunc()
	}
	if err != nil {
		return nil, err
	}

	timeout := 4 * time.Minute

	if !loggedIn {
		// 记录图片格式，方便排查
		imgType := "url"
		if strings.HasPrefix(img, "data:") {
			imgType = "base64"
		}
		logrus.Infof("main login: qrcode type=%s len=%d, waiting for scan...", imgType, len(img))

		go func() {
			ctxTimeout, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			defer func() {
				logrus.Info("main login: browser session closing")
				deferFunc()
			}()

			if loginAction.WaitForLogin(ctxTimeout) {
				logrus.Info("main login: WaitForLogin returned true, saving cookies")
				if er := saveCookiesForBot(page, botID); er != nil {
					logrus.Errorf("failed to save cookies: %v", er)
				} else {
					logrus.Info("main login: cookies saved successfully")
					notifyLoginSuccess(notifySession, "main")
				}
			} else {
				logrus.Warn("main login: WaitForLogin timed out or cancelled")
			}
		}()
	}

	return &LoginQrcodeResponse{
		Timeout: func() string {
			if loggedIn {
				return "0s"
			}
			return timeout.String()
		}(),
		Img:        img,
		IsLoggedIn: loggedIn,
	}, nil
}

// PublishContent 发布内容
func (s *XiaohongshuService) PublishContent(ctx context.Context, botID string, req *PublishRequest) (*PublishResponse, error) {
	// 验证标题长度（小红书限制：最大20个字）
	if xhsutil.CalcTitleLength(req.Title) > 20 {
		return nil, fmt.Errorf("标题长度超过限制")
	}

	// 处理图片：下载URL图片或使用本地路径
	imagePaths, err := s.processImages(req.Images)
	if err != nil {
		return nil, err
	}

	// 解析定时发布时间
	var scheduleTime *time.Time
	if req.ScheduleAt != "" {
		t, err := time.Parse(time.RFC3339, req.ScheduleAt)
		if err != nil {
			return nil, fmt.Errorf("定时发布时间格式错误，请使用 ISO8601 格式: %v", err)
		}

		// 校验定时发布时间范围：1小时至14天
		now := time.Now()
		minTime := now.Add(1 * time.Hour)
		maxTime := now.Add(14 * 24 * time.Hour)

		if t.Before(minTime) {
			return nil, fmt.Errorf("定时发布时间必须至少在1小时后，当前设置: %s，最早可选: %s",
				t.Format("2006-01-02 15:04"), minTime.Format("2006-01-02 15:04"))
		}
		if t.After(maxTime) {
			return nil, fmt.Errorf("定时发布时间不能超过14天，当前设置: %s，最晚可选: %s",
				t.Format("2006-01-02 15:04"), maxTime.Format("2006-01-02 15:04"))
		}

		scheduleTime = &t
		logrus.Infof("设置定时发布时间: %s", t.Format("2006-01-02 15:04"))
	}

	// 构建发布内容
	content := xiaohongshu.PublishImageContent{
		Title:        req.Title,
		Content:      req.Content,
		Tags:         req.Tags,
		ImagePaths:   imagePaths,
		ScheduleTime: scheduleTime,
		IsOriginal:   req.IsOriginal,
		Visibility:   req.Visibility,
		Products:     req.Products,
	}

	// 执行发布
	if err := s.publishContent(ctx, botID, content); err != nil {
		logrus.Errorf("发布内容失败: title=%s %v", content.Title, err)
		return nil, err
	}

	response := &PublishResponse{
		Title:   req.Title,
		Content: req.Content,
		Images:  len(imagePaths),
		Status:  "发布完成",
	}

	return response, nil
}

// processImages 处理图片列表，支持URL下载和本地路径
func (s *XiaohongshuService) processImages(images []string) ([]string, error) {
	processor := downloader.NewImageProcessor()
	return processor.ProcessImages(images)
}

// publishContent 执行内容发布
func (s *XiaohongshuService) publishContent(ctx context.Context, botID string, content xiaohongshu.PublishImageContent) error {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action, err := xiaohongshu.NewPublishImageAction(page)
	if err != nil {
		return err
	}

	// 执行发布
	return action.Publish(ctx, content)
}

// PublishVideo 发布视频（本地文件）
func (s *XiaohongshuService) PublishVideo(ctx context.Context, botID string, req *PublishVideoRequest) (*PublishVideoResponse, error) {
	// 标题长度校验（小红书限制：最大20个字）
	if xhsutil.CalcTitleLength(req.Title) > 20 {
		return nil, fmt.Errorf("标题长度超过限制")
	}

	// 本地视频文件校验
	if req.Video == "" {
		return nil, fmt.Errorf("必须提供本地视频文件")
	}
	if _, err := os.Stat(req.Video); err != nil {
		return nil, fmt.Errorf("视频文件不存在或不可访问: %v", err)
	}

	// 解析定时发布时间
	var scheduleTime *time.Time
	if req.ScheduleAt != "" {
		t, err := time.Parse(time.RFC3339, req.ScheduleAt)
		if err != nil {
			return nil, fmt.Errorf("定时发布时间格式错误，请使用 ISO8601 格式: %v", err)
		}

		// 校验定时发布时间范围：1小时至14天
		now := time.Now()
		minTime := now.Add(1 * time.Hour)
		maxTime := now.Add(14 * 24 * time.Hour)

		if t.Before(minTime) {
			return nil, fmt.Errorf("定时发布时间必须至少在1小时后，当前设置: %s，最早可选: %s",
				t.Format("2006-01-02 15:04"), minTime.Format("2006-01-02 15:04"))
		}
		if t.After(maxTime) {
			return nil, fmt.Errorf("定时发布时间不能超过14天，当前设置: %s，最晚可选: %s",
				t.Format("2006-01-02 15:04"), maxTime.Format("2006-01-02 15:04"))
		}

		scheduleTime = &t
		logrus.Infof("设置定时发布时间: %s", t.Format("2006-01-02 15:04"))
	}

	// 构建发布内容
	content := xiaohongshu.PublishVideoContent{
		Title:        req.Title,
		Content:      req.Content,
		Tags:         req.Tags,
		VideoPath:    req.Video,
		ScheduleTime: scheduleTime,
		Visibility:   req.Visibility,
		Products:     req.Products,
	}

	// 执行发布
	if err := s.publishVideo(ctx, botID, content); err != nil {
		return nil, err
	}

	resp := &PublishVideoResponse{
		Title:   req.Title,
		Content: req.Content,
		Video:   req.Video,
		Status:  "发布完成",
	}
	return resp, nil
}

// publishVideo 执行视频发布
func (s *XiaohongshuService) publishVideo(ctx context.Context, botID string, content xiaohongshu.PublishVideoContent) error {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action, err := xiaohongshu.NewPublishVideoAction(page)
	if err != nil {
		return err
	}

	return action.PublishVideo(ctx, content)
}

// ListFeeds 获取Feeds列表
func (s *XiaohongshuService) ListFeeds(ctx context.Context, botID string) (*FeedsListResponse, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	// 创建 Feeds 列表 action
	action := xiaohongshu.NewFeedsListAction(page)

	// 获取 Feeds 列表
	feeds, err := action.GetFeedsList(ctx)
	if err != nil {
		logrus.Errorf("获取 Feeds 列表失败: %v", err)
		return nil, err
	}

	response := &FeedsListResponse{
		Feeds: feeds,
		Count: len(feeds),
	}

	return response, nil
}

func (s *XiaohongshuService) SearchFeeds(ctx context.Context, botID string, keyword string, filters ...xiaohongshu.FilterOption) (*FeedsListResponse, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewSearchAction(page)

	feeds, err := action.Search(ctx, keyword, filters...)
	if err != nil {
		return nil, err
	}

	response := &FeedsListResponse{
		Feeds: feeds,
		Count: len(feeds),
	}

	return response, nil
}

// GetFeedDetail 获取Feed详情
func (s *XiaohongshuService) GetFeedDetail(ctx context.Context, botID string, feedID, xsecToken string, loadAllComments bool) (*FeedDetailResponse, error) {
	return s.GetFeedDetailWithConfig(ctx, botID, feedID, xsecToken, loadAllComments, xiaohongshu.DefaultCommentLoadConfig())
}

// GetFeedDetailWithConfig 使用配置获取Feed详情
func (s *XiaohongshuService) GetFeedDetailWithConfig(ctx context.Context, botID string, feedID, xsecToken string, loadAllComments bool, config xiaohongshu.CommentLoadConfig) (*FeedDetailResponse, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	// 创建 Feed 详情 action
	action := xiaohongshu.NewFeedDetailAction(page)

	// 获取 Feed 详情
	result, err := action.GetFeedDetailWithConfig(ctx, feedID, xsecToken, loadAllComments, config)
	if err != nil {
		return nil, err
	}

	response := &FeedDetailResponse{
		FeedID: feedID,
		Data:   result,
	}

	return response, nil
}

// UserProfile 获取用户信息
func (s *XiaohongshuService) UserProfile(ctx context.Context, botID string, userID, xsecToken string) (*UserProfileResponse, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewUserProfileAction(page)

	result, err := action.UserProfile(ctx, userID, xsecToken)
	if err != nil {
		return nil, err
	}
	response := &UserProfileResponse{
		UserBasicInfo: result.UserBasicInfo,
		Interactions:  result.Interactions,
		Feeds:         result.Feeds,
	}

	return response, nil

}

// PostCommentToFeed 发表评论到Feed
func (s *XiaohongshuService) PostCommentToFeed(ctx context.Context, botID string, feedID, xsecToken, content string) (*PostCommentResponse, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewCommentFeedAction(page)

	if err := action.PostComment(ctx, feedID, xsecToken, content); err != nil {
		return nil, err
	}

	return &PostCommentResponse{FeedID: feedID, Success: true, Message: "评论发表成功"}, nil
}

// LikeFeed 点赞笔记
func (s *XiaohongshuService) LikeFeed(ctx context.Context, botID string, feedID, xsecToken string) (*ActionResult, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewLikeAction(page)
	if err := action.Like(ctx, feedID, xsecToken); err != nil {
		return nil, err
	}
	return &ActionResult{FeedID: feedID, Success: true, Message: "点赞成功或已点赞"}, nil
}

// UnlikeFeed 取消点赞笔记
func (s *XiaohongshuService) UnlikeFeed(ctx context.Context, botID string, feedID, xsecToken string) (*ActionResult, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewLikeAction(page)
	if err := action.Unlike(ctx, feedID, xsecToken); err != nil {
		return nil, err
	}
	return &ActionResult{FeedID: feedID, Success: true, Message: "取消点赞成功或未点赞"}, nil
}

// FavoriteFeed 收藏笔记
func (s *XiaohongshuService) FavoriteFeed(ctx context.Context, botID string, feedID, xsecToken string) (*ActionResult, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewFavoriteAction(page)
	if err := action.Favorite(ctx, feedID, xsecToken); err != nil {
		return nil, err
	}
	return &ActionResult{FeedID: feedID, Success: true, Message: "收藏成功或已收藏"}, nil
}

// UnfavoriteFeed 取消收藏笔记
func (s *XiaohongshuService) UnfavoriteFeed(ctx context.Context, botID string, feedID, xsecToken string) (*ActionResult, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewFavoriteAction(page)
	if err := action.Unfavorite(ctx, feedID, xsecToken); err != nil {
		return nil, err
	}
	return &ActionResult{FeedID: feedID, Success: true, Message: "取消收藏成功或未收藏"}, nil
}

// ReplyCommentToFeed 回复指定评论
func (s *XiaohongshuService) ReplyCommentToFeed(ctx context.Context, botID string, feedID, xsecToken, commentID, userID, content string) (*ReplyCommentResponse, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewCommentFeedAction(page)

	if err := action.ReplyToComment(ctx, feedID, xsecToken, commentID, userID, content); err != nil {
		return nil, err
	}

	return &ReplyCommentResponse{
		FeedID:          feedID,
		TargetCommentID: commentID,
		TargetUserID:    userID,
		Success:         true,
		Message:         "评论回复成功",
	}, nil
}

func newBrowser() *browser.Browser {
	return newBrowserForBot("")
}

// newBrowserForBot 为指定 bot 创建浏览器实例。
// botID 为空时 fallback 到全局配置（兼容单实例模式）。
func newBrowserForBot(botID string) *browser.Browser {
	opts := []browser.Option{browser.WithBinPath(configs.GetBinPath())}

	if botID != "" {
		// 多租户模式：使用 per-bot 的 cookie 和 profile
		opts = append(opts, browser.WithCookiePath(cookies.GetCookiesFilePathForBot(botID)))
		if d := configs.GetProfileDirForBot(botID); d != "" {
			opts = append(opts, browser.WithProfileDir(d))
		}
	} else {
		// 兼容模式：使用全局配置
		if d := configs.GetProfileDir(); d != "" {
			opts = append(opts, browser.WithProfileDir(d))
		}
	}

	return browser.NewBrowser(configs.IsHeadless(), opts...)
}

func saveCookies(page *rod.Page) error {
	return saveCookiesForBot(page, "")
}

// saveCookiesForBot 保存 cookies 到指定 bot 的 cookie 文件。
func saveCookiesForBot(page *rod.Page, botID string) error {
	cks, err := page.Browser().GetCookies()
	if err != nil {
		return err
	}

	data, err := json.Marshal(cks)
	if err != nil {
		return err
	}

	cookieLoader := cookies.NewLoadCookie(cookies.GetCookiesFilePathForBot(botID))
	return cookieLoader.SaveCookies(data)
}

// withBrowserPage 执行需要浏览器页面的操作的通用函数
func withBrowserPage(fn func(*rod.Page) error) error {
	return withBrowserPageForBot("", fn)
}

// withBrowserPageForBot 为指定 bot 执行浏览器操作
func withBrowserPageForBot(botID string, fn func(*rod.Page) error) error {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	return fn(page)
}

// CheckBothLoginStatus 用单个 browser 同时检查主站 + 创作者平台登录状态
func (s *XiaohongshuService) CheckBothLoginStatus(ctx context.Context, botID string) (mainOK bool, creatorOK bool) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	// 主站和创作者平台共享 .xiaohongshu.com cookie，统一用 web_session 判断
	cks, err := page.Browser().GetCookies()
	if err == nil {
		for _, c := range cks {
			if c.Name == "web_session" && c.Value != "" {
				mainOK = true
				creatorOK = true
				break
			}
		}
	}

	return
}

// CheckCreatorLoginStatus 检查创作者平台登录状态（单独使用时）
func (s *XiaohongshuService) CheckCreatorLoginStatus(ctx context.Context, botID string) bool {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	pp := page.Context(ctx).Timeout(30 * time.Second)
	if err := pp.Navigate("https://creator.xiaohongshu.com/publish/publish?source=official"); err != nil {
		logrus.Warnf("创作者平台导航失败: %v", err)
		return false
	}
	if err := pp.WaitLoad(); err != nil {
		logrus.Warnf("创作者平台页面加载失败: %v", err)
		return false
	}
	time.Sleep(2 * time.Second)

	info, err := pp.Info()
	if err != nil {
		return false
	}
	return !strings.Contains(info.URL, "/login")
}

// GetCreatorLoginQrcode 获取创作者平台登录二维码
func (s *XiaohongshuService) GetCreatorLoginQrcode(ctx context.Context, botID string, notifySession string) (*LoginQrcodeResponse, error) {
	b := newBrowserForBot(botID)
	page := b.NewPage()

	deferFunc := func() {
		_ = page.Close()
		b.Close()
	}

	pp := page.Context(ctx).Timeout(60 * time.Second)

	// 预热：先访问主站让 XHS 下发基础追踪 cookie，降低创作者平台反爬概率
	logrus.Info("creator login: 预热主站...")
	if err := pp.Navigate("https://www.xiaohongshu.com"); err != nil {
		logrus.Warnf("creator login: 预热主站失败: %v", err)
	} else {
		_ = pp.WaitLoad()
		time.Sleep(2 * time.Second)
	}

	if err := pp.Navigate("https://creator.xiaohongshu.com/login?source=official"); err != nil {
		defer deferFunc()
		return nil, fmt.Errorf("导航到创作者登录页失败: %w", err)
	}
	if err := pp.WaitLoad(); err != nil {
		logrus.Warnf("等待创作者登录页加载: %v", err)
	}
	time.Sleep(2 * time.Second)

	// 已登录则直接返回
	info, err := pp.Info()
	if err != nil {
		defer deferFunc()
		return nil, fmt.Errorf("获取页面信息失败: %w", err)
	}
	if !strings.Contains(info.URL, "/login") {
		defer deferFunc()
		return &LoginQrcodeResponse{Timeout: "0s", IsLoggedIn: true}, nil
	}

	// 切换到扫码登录
	switchBtn, err := pp.Timeout(10 * time.Second).Element(".css-wemwzq")
	if err != nil {
		defer deferFunc()
		return nil, fmt.Errorf("未找到扫码切换按钮: %w", err)
	}
	if err := switchBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		defer deferFunc()
		return nil, fmt.Errorf("点击扫码切换按钮失败: %w", err)
	}
	time.Sleep(2 * time.Second)

	qrElem, err := pp.Timeout(10 * time.Second).Element(".css-1lhmg90, img[class*=qrcode], .qrcode-img")
	if err != nil {
		defer deferFunc()
		return nil, fmt.Errorf("未找到创作者平台二维码: %w", err)
	}
	src, err := qrElem.Attribute("src")
	if err != nil || src == nil || *src == "" {
		defer deferFunc()
		return nil, fmt.Errorf("创作者平台二维码 src 为空")
	}

	timeout := 4 * time.Minute

	go func() {
		ctxTimeout, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		defer deferFunc()

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctxTimeout.Done():
				return
			case <-ticker.C:
				info, err := pp.Info()
				if err != nil {
					continue
				}
				logrus.Infof("creator login poll: url=%s", info.URL)
				if !strings.Contains(info.URL, "/login") {
					if er := saveCookiesForBot(page, botID); er != nil {
						logrus.Errorf("failed to save creator cookies: %v", er)
					} else {
						notifyLoginSuccess(notifySession, "creator")
					}
					return
				}
			}
		}
	}()

	return &LoginQrcodeResponse{
		Timeout:    timeout.String(),
		Img:        *src,
		IsLoggedIn: false,
	}, nil
}

// GetCreatorHome 获取创作者首页数据
func (s *XiaohongshuService) GetCreatorHome(ctx context.Context, botID string) (*xiaohongshu.CreatorHomeInfo, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewCreatorStatsAction(page)
	return action.GetCreatorHome(ctx)
}

// ListNotes 列出创作者后台笔记
func (s *XiaohongshuService) ListNotes(ctx context.Context, botID string) ([]xiaohongshu.NoteInfo, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewNoteManageAction(page)
	return action.ListNotes(ctx)
}

// DeleteNote 删除笔记
func (s *XiaohongshuService) DeleteNote(ctx context.Context, botID string, feedID string) error {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewNoteManageAction(page)
	return action.Delete(ctx, feedID)
}

// PinNote 置顶笔记
func (s *XiaohongshuService) PinNote(ctx context.Context, botID string, feedID string) error {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewNoteManageAction(page)
	return action.Pin(ctx, feedID)
}

// PublishLongform 发布长文
func (s *XiaohongshuService) PublishLongform(ctx context.Context, botID string, title, content, desc string, tags []string, visibility string) error {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action, err := xiaohongshu.NewPublishLongformAction(page)
	if err != nil {
		return err
	}

	return action.PublishLongform(ctx, xiaohongshu.PublishLongformContent{
		Title:      title,
		Content:    content,
		Desc:       desc,
		Tags:       tags,
		Visibility: visibility,
	})
}

// PublishTextToImage 文字配图发布
func (s *XiaohongshuService) PublishTextToImage(ctx context.Context, botID string, title, content string, textCards []string, imageStyle string, tags []string, isOriginal bool, visibility string, scheduleAt string) error {
	if xhsutil.CalcTitleLength(title) > 20 {
		return fmt.Errorf("标题长度超过限制")
	}

	var scheduleTime *time.Time
	if scheduleAt != "" {
		t, err := time.Parse(time.RFC3339, scheduleAt)
		if err != nil {
			return fmt.Errorf("定时发布时间格式错误: %v", err)
		}
		scheduleTime = &t
	}

	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action, err := xiaohongshu.NewPublishTextImageAction(page)
	if err != nil {
		return err
	}

	return action.Publish(ctx, xiaohongshu.PublishTextImageContent{
		Title:        title,
		Content:      content,
		TextCards:    textCards,
		ImageStyle:   imageStyle,
		Tags:         tags,
		IsOriginal:   isOriginal,
		Visibility:   visibility,
		ScheduleTime: scheduleTime,
	})
}

// GetNotificationComments 获取通知评论列表
func (s *XiaohongshuService) GetNotificationComments(ctx context.Context, botID string) (*xiaohongshu.NotificationListResponse, error) {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewNotificationAction(page)
	return action.GetNotificationComments(ctx)
}

// ReplyNotificationComment 通知页回复评论
func (s *XiaohongshuService) ReplyNotificationComment(ctx context.Context, botID string, commentIndex int, content string) error {
	b := newBrowserForBot(botID)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewNotificationAction(page)
	return action.ReplyToNotificationComment(ctx, commentIndex, content)
}

// GetMyProfile 获取当前登录用户的个人信息
func (s *XiaohongshuService) GetMyProfile(ctx context.Context, botID string) (*UserProfileResponse, error) {
	var result *xiaohongshu.UserProfileResponse
	var err error

	err = withBrowserPageForBot(botID, func(page *rod.Page) error {
		action := xiaohongshu.NewUserProfileAction(page)
		result, err = action.GetMyProfileViaSidebar(ctx)
		return err
	})

	if err != nil {
		return nil, err
	}

	response := &UserProfileResponse{
		UserBasicInfo: result.UserBasicInfo,
		Interactions:  result.Interactions,
		Feeds:         result.Feeds,
	}

	return response, nil
}

// notifyLoginSuccess 通过 openclaw agent 向指定 session 回传登录成功通知。
// sessionKey 为空时静默跳过；loginType 区分平台："main" 主站 / "creator" 创作者平台。
func notifyLoginSuccess(sessionKey, loginType string) {
	if sessionKey == "" {
		return
	}
	msg := "✅ 小红书主站登录 Cookie 已保存，可以开始操作了"
	if loginType == "creator" {
		msg = "✅ 小红书创作者平台登录 Cookie 已保存，可以开始操作了"
	}
	go func() {
		cmd := exec.Command("openclaw", "agent",
			"--session-key", sessionKey,
			"--message", msg,
			"--deliver",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			logrus.Warnf("notifyLoginSuccess: openclaw 通知失败: %v, output: %s", err, out)
		}
	}()
}
