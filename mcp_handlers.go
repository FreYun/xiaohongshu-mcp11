package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

const qrMediaDir = "/home/rooot/.openclaw/media"

// saveQrImage 保存 QR 图片到 openclaw media 目录，供 bot 通过文件路径发送给用户。
// prefix: "xhs-qr" (主站) 或 "xhs-creator-qr" (创作者平台)
func (s *AppServer) saveQrImage(base64Data, prefix, botID string) {
	if botID == "" {
		return
	}

	raw, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		logrus.Warnf("saveQrImage: decode failed: %v", err)
		return
	}

	_ = os.MkdirAll(qrMediaDir, 0755)
	path := filepath.Join(qrMediaDir, fmt.Sprintf("%s-%s.png", prefix, botID))
	if err := os.WriteFile(path, raw, 0644); err != nil {
		logrus.Warnf("saveQrImage: write failed: %v", err)
		return
	}
	logrus.Infof("QR image saved: %s", path)
}

// MCP 工具处理函数

// parseVisibility 从 MCP 参数中解析可见范围
func parseVisibility(args map[string]interface{}) string {
	v, ok := args["visibility"]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// handleCheckLoginStatus 处理检查登录状态（单个 browser 实例同时检查主站 + 创作者平台）
func (s *AppServer) handleCheckLoginStatus(ctx context.Context, botID string) *MCPToolResult {
	logrus.Infof("MCP [%s]: 检查登录状态", botID)

	mainOK, creatorOK := s.xiaohongshuService.CheckBothLoginStatus(ctx, botID)

	mainIcon := "❌ 未登录"
	if mainOK {
		mainIcon = "✅ 已登录"
	}
	creatorIcon := "❌ 未登录"
	if creatorOK {
		creatorIcon = "✅ 已登录"
	}

	resultText := fmt.Sprintf("账号: %s\n主站: %s\n创作者平台: %s", configs.Username, mainIcon, creatorIcon)

	if !mainOK && !creatorOK {
		resultText += "\n\n请使用 get_both_login_qrcodes 同时获取两张二维码登录。"
	} else if !mainOK {
		resultText += "\n\n请使用 get_login_qrcode 扫码登录主站。"
	} else if !creatorOK {
		resultText += "\n\n请使用 get_creator_login_qrcode 扫码登录创作者平台。"
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handleGetLoginQrcode 处理获取登录二维码请求。
// 返回二维码图片的 Base64 编码和超时时间，供前端展示扫码登录。
// notifySession：可选，扫码成功后通过该 session key 回传通知。
func (s *AppServer) handleGetLoginQrcode(ctx context.Context, botID string, notifySession string) *MCPToolResult {
	logrus.Infof("MCP [%s]: 获取登录扫码图片", botID)

	result, err := s.xiaohongshuService.GetLoginQrcode(ctx, botID, notifySession)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "获取登录扫码图片失败: " + err.Error()}},
			IsError: true,
		}
	}

	if result.IsLoggedIn {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "你当前已处于登录状态"}},
		}
	}

	now := time.Now()
	deadline := func() string {
		d, err := time.ParseDuration(result.Timeout)
		if err != nil {
			return now.Format("2006-01-02 15:04:05")
		}
		return now.Add(d).Format("2006-01-02 15:04:05")
	}()

	base64Data := strings.TrimPrefix(result.Img, "data:image/png;base64,")

	// 保存 QR 图片到 media 目录，供 bot 通过文件路径发送
	s.saveQrImage(base64Data, "xhs-qr", botID)

	contents := []MCPContent{
		{Type: "text", Text: "请用小红书 App 在 " + deadline + " 前扫码登录 👇"},
		{
			Type:     "image",
			MimeType: "image/png",
			Data:     base64Data,
		},
	}
	return &MCPToolResult{Content: contents}
}

// handleDeleteCookies 处理删除 cookies 请求，用于登录重置
func (s *AppServer) handleDeleteCookies(ctx context.Context, botID string) *MCPToolResult {
	logrus.Infof("MCP [%s]: 删除 cookies，重置登录状态", botID)

	err := s.xiaohongshuService.DeleteCookies(ctx, botID)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "删除 cookies 失败: " + err.Error()}},
			IsError: true,
		}
	}

	cookiePath := cookies.GetCookiesFilePathForBot(botID)
	resultText := fmt.Sprintf("Cookies 已成功删除，登录状态已重置。\n\n删除的文件路径: %s\n\n下次操作时，需要重新登录。", cookiePath)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handlePublishContent 处理发布内容
func (s *AppServer) handlePublishContent(ctx context.Context, botID string, args map[string]interface{}) *MCPToolResult {
	logrus.Infof("MCP [%s]: 发布内容", botID)

	// 解析参数
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	imagePathsInterface, _ := args["images"].([]interface{})
	tagsInterface, _ := args["tags"].([]interface{})
	productsInterface, _ := args["products"].([]interface{})

	var imagePaths []string
	for _, path := range imagePathsInterface {
		if pathStr, ok := path.(string); ok {
			imagePaths = append(imagePaths, pathStr)
		}
	}

	var tags []string
	for _, tag := range tagsInterface {
		if tagStr, ok := tag.(string); ok {
			tags = append(tags, tagStr)
		}
	}

	var products []string
	for _, p := range productsInterface {
		if pStr, ok := p.(string); ok {
			products = append(products, pStr)
		}
	}

	// 解析定时发布参数
	scheduleAt, _ := args["schedule_at"].(string)
	visibility := parseVisibility(args)

	// 解析原创参数
	isOriginal, _ := args["is_original"].(bool)

	logrus.Infof("MCP: 发布内容 - 标题: %s, 图片数量: %d, 标签数量: %d, 定时: %s, 原创: %v, visibility: %s, 商品: %v", title, len(imagePaths), len(tags), scheduleAt, isOriginal, visibility, products)

	// 构建发布请求
	req := &PublishRequest{
		Title:      title,
		Content:    content,
		Images:     imagePaths,
		Tags:       tags,
		ScheduleAt: scheduleAt,
		IsOriginal: isOriginal,
		Visibility: visibility,
		Products:   products,
	}

	// 执行发布
	result, err := s.xiaohongshuService.PublishContent(ctx, botID, req)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发布失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	resultText := fmt.Sprintf("内容发布成功: %+v", result)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handlePublishVideo 处理发布视频内容（仅本地单个视频文件）
func (s *AppServer) handlePublishVideo(ctx context.Context, botID string, args map[string]interface{}) *MCPToolResult {
	logrus.Infof("MCP [%s]: 发布视频内容（本地）", botID)

	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	videoPath, _ := args["video"].(string)
	tagsInterface, _ := args["tags"].([]interface{})
	productsInterface, _ := args["products"].([]interface{})

	var tags []string
	for _, tag := range tagsInterface {
		if tagStr, ok := tag.(string); ok {
			tags = append(tags, tagStr)
		}
	}

	var products []string
	for _, p := range productsInterface {
		if pStr, ok := p.(string); ok {
			products = append(products, pStr)
		}
	}

	if videoPath == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发布失败: 缺少本地视频文件路径",
			}},
			IsError: true,
		}
	}

	// 解析定时发布参数
	scheduleAt, _ := args["schedule_at"].(string)
	visibility := parseVisibility(args)

	logrus.Infof("MCP: 发布视频 - 标题: %s, 标签数量: %d, 定时: %s, visibility: %s, 商品: %v", title, len(tags), scheduleAt, visibility, products)

	// 构建发布请求
	req := &PublishVideoRequest{
		Title:      title,
		Content:    content,
		Video:      videoPath,
		Tags:       tags,
		ScheduleAt: scheduleAt,
		Visibility: visibility,
		Products:   products,
	}

	// 执行发布
	result, err := s.xiaohongshuService.PublishVideo(ctx, botID, req)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发布失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	resultText := fmt.Sprintf("视频发布成功: %+v", result)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handleListFeeds 处理获取Feeds列表
func (s *AppServer) handleListFeeds(ctx context.Context, botID string) *MCPToolResult {
	logrus.Infof("MCP [%s]: 获取Feeds列表", botID)

	result, err := s.xiaohongshuService.ListFeeds(ctx, botID)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feeds列表失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("获取Feeds列表成功，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleSearchFeeds 处理搜索Feeds
func (s *AppServer) handleSearchFeeds(ctx context.Context, botID string, args SearchFeedsArgs) *MCPToolResult {
	logrus.Infof("MCP [%s]: 搜索Feeds", botID)

	if args.Keyword == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "搜索Feeds失败: 缺少关键词参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 搜索Feeds - 关键词: %s", args.Keyword)

	// 将 MCP 的 FilterOption 转换为 xiaohongshu.FilterOption
	filter := xiaohongshu.FilterOption{
		SortBy:      args.Filters.SortBy,
		NoteType:    args.Filters.NoteType,
		PublishTime: args.Filters.PublishTime,
		SearchScope: args.Filters.SearchScope,
		Location:    args.Filters.Location,
	}

	result, err := s.xiaohongshuService.SearchFeeds(ctx, botID, args.Keyword, filter)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "搜索Feeds失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("搜索Feeds成功，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleGetFeedDetail 处理获取Feed详情
func (s *AppServer) handleGetFeedDetail(ctx context.Context, botID string, args map[string]any) *MCPToolResult {
	logrus.Infof("MCP [%s]: 获取Feed详情", botID)

	// 解析参数
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feed详情失败: 缺少feed_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feed详情失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	loadAll := false
	if raw, ok := args["load_all_comments"]; ok {
		switch v := raw.(type) {
		case bool:
			loadAll = v
		case string:
			if parsed, err := strconv.ParseBool(v); err == nil {
				loadAll = parsed
			}
		case float64:
			loadAll = v != 0
		}
	}

	// 解析评论配置参数，如果未提供则使用默认值
	config := xiaohongshu.DefaultCommentLoadConfig()

	if raw, ok := args["click_more_replies"]; ok {
		switch v := raw.(type) {
		case bool:
			config.ClickMoreReplies = v
		case string:
			if parsed, err := strconv.ParseBool(v); err == nil {
				config.ClickMoreReplies = parsed
			}
		}
	}

	if raw, ok := args["max_replies_threshold"]; ok {
		switch v := raw.(type) {
		case float64:
			config.MaxRepliesThreshold = int(v)
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				config.MaxRepliesThreshold = parsed
			}
		case int:
			config.MaxRepliesThreshold = v
		}
	}

	if raw, ok := args["max_comment_items"]; ok {
		switch v := raw.(type) {
		case float64:
			config.MaxCommentItems = int(v)
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				config.MaxCommentItems = parsed
			}
		case int:
			config.MaxCommentItems = v
		}
	}

	if raw, ok := args["scroll_speed"].(string); ok && raw != "" {
		config.ScrollSpeed = raw
	}

	logrus.Infof("MCP: 获取Feed详情 - Feed ID: %s, loadAllComments=%v, config=%+v", feedID, loadAll, config)

	result, err := s.xiaohongshuService.GetFeedDetailWithConfig(ctx, botID, feedID, xsecToken, loadAll, config)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取Feed详情失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("获取Feed详情成功，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleUserProfile 获取用户主页
func (s *AppServer) handleUserProfile(ctx context.Context, botID string, args map[string]any) *MCPToolResult {
	logrus.Infof("MCP [%s]: 获取用户主页", botID)

	// 解析参数
	userID, ok := args["user_id"].(string)
	if !ok || userID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取用户主页失败: 缺少user_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取用户主页失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 获取用户主页 - User ID: %s", userID)

	result, err := s.xiaohongshuService.UserProfile(ctx, botID, userID, xsecToken)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "获取用户主页失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 格式化输出，转换为JSON字符串
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("获取用户主页，但序列化失败: %v", err),
			}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}
}

// handleLikeFeed 处理点赞/取消点赞
func (s *AppServer) handleLikeFeed(ctx context.Context, botID string, args map[string]interface{}) *MCPToolResult {
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少feed_id参数"}}, IsError: true}
	}
	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少xsec_token参数"}}, IsError: true}
	}
	unlike, _ := args["unlike"].(bool)

	var res *ActionResult
	var err error

	if unlike {
		res, err = s.xiaohongshuService.UnlikeFeed(ctx, botID, feedID, xsecToken)
	} else {
		res, err = s.xiaohongshuService.LikeFeed(ctx, botID, feedID, xsecToken)
	}

	if err != nil {
		action := "点赞"
		if unlike {
			action = "取消点赞"
		}
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: action + "失败: " + err.Error()}}, IsError: true}
	}

	action := "点赞"
	if unlike {
		action = "取消点赞"
	}
	return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("%s成功 - Feed ID: %s", action, res.FeedID)}}}
}

// handleFavoriteFeed 处理收藏/取消收藏
func (s *AppServer) handleFavoriteFeed(ctx context.Context, botID string, args map[string]interface{}) *MCPToolResult {
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少feed_id参数"}}, IsError: true}
	}
	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: "操作失败: 缺少xsec_token参数"}}, IsError: true}
	}
	unfavorite, _ := args["unfavorite"].(bool)

	var res *ActionResult
	var err error

	if unfavorite {
		res, err = s.xiaohongshuService.UnfavoriteFeed(ctx, botID, feedID, xsecToken)
	} else {
		res, err = s.xiaohongshuService.FavoriteFeed(ctx, botID, feedID, xsecToken)
	}

	if err != nil {
		action := "收藏"
		if unfavorite {
			action = "取消收藏"
		}
		return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: action + "失败: " + err.Error()}}, IsError: true}
	}

	action := "收藏"
	if unfavorite {
		action = "取消收藏"
	}
	return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("%s成功 - Feed ID: %s", action, res.FeedID)}}}
}

// handlePostComment 处理发表评论到Feed
func (s *AppServer) handlePostComment(ctx context.Context, botID string, args map[string]interface{}) *MCPToolResult {
	logrus.Infof("MCP [%s]: 发表评论到Feed", botID)

	// 解析参数
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: 缺少feed_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: 缺少content参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 发表评论 - Feed ID: %s, 内容长度: %d", feedID, len(content))

	// 发表评论
	result, err := s.xiaohongshuService.PostCommentToFeed(ctx, botID, feedID, xsecToken, content)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "发表评论失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 返回成功结果，只包含feed_id
	resultText := fmt.Sprintf("评论发表成功 - Feed ID: %s", result.FeedID)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: resultText,
		}},
	}
}

// handleGetBothLoginQrcodes 同时获取主站+创作者平台二维码
func (s *AppServer) handleGetBothLoginQrcodes(ctx context.Context, botID string) *MCPToolResult {
	logrus.Infof("MCP [%s]: 同时获取主站+创作者平台二维码", botID)

	// 主站二维码（批量获取时无 notifySession）
	mainResult, mainErr := s.xiaohongshuService.GetLoginQrcode(ctx, botID, "")
	// 创作者平台二维码
	creatorResult, creatorErr := s.xiaohongshuService.GetCreatorLoginQrcode(ctx, botID, "")

	var contents []MCPContent

	if mainErr != nil {
		contents = append(contents, MCPContent{Type: "text", Text: "主站二维码获取失败: " + mainErr.Error()})
	} else if mainResult.IsLoggedIn {
		contents = append(contents, MCPContent{Type: "text", Text: "主站: ✅ 已登录"})
	} else {
		mainBase64 := strings.TrimPrefix(mainResult.Img, "data:image/png;base64,")
		s.saveQrImage(mainBase64, "xhs-qr", botID)
		contents = append(contents, MCPContent{Type: "text", Text: "主站登录二维码 👇"})
		contents = append(contents, MCPContent{
			Type:     "image",
			MimeType: "image/png",
			Data:     mainBase64,
		})
	}

	if creatorErr != nil {
		contents = append(contents, MCPContent{Type: "text", Text: "创作者平台二维码获取失败: " + creatorErr.Error()})
	} else if creatorResult.IsLoggedIn {
		contents = append(contents, MCPContent{Type: "text", Text: "创作者平台: ✅ 已登录"})
	} else {
		creatorBase64 := strings.TrimPrefix(creatorResult.Img, "data:image/png;base64,")
		s.saveQrImage(creatorBase64, "xhs-creator-qr", botID)
		contents = append(contents, MCPContent{Type: "text", Text: "创作者平台登录二维码 👇"})
		contents = append(contents, MCPContent{
			Type:     "image",
			MimeType: "image/png",
			Data:     creatorBase64,
		})
	}

	return &MCPToolResult{Content: contents}
}

// handleGetCreatorLoginQrcode 获取创作者平台登录二维码
func (s *AppServer) handleGetCreatorLoginQrcode(ctx context.Context, botID string, notifySession string) *MCPToolResult {
	logrus.Infof("MCP [%s]: 获取创作者平台登录二维码", botID)

	result, err := s.xiaohongshuService.GetCreatorLoginQrcode(ctx, botID, notifySession)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "获取创作者平台二维码失败: " + err.Error()}},
			IsError: true,
		}
	}

	if result.IsLoggedIn {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "创作者平台当前已登录"}},
		}
	}

	now := time.Now()
	deadline := func() string {
		d, err := time.ParseDuration(result.Timeout)
		if err != nil {
			return now.Format("2006-01-02 15:04:05")
		}
		return now.Add(d).Format("2006-01-02 15:04:05")
	}()

	creatorBase64 := strings.TrimPrefix(result.Img, "data:image/png;base64,")

	// 保存创作者平台 QR 图片到 media 目录
	s.saveQrImage(creatorBase64, "xhs-creator-qr", botID)

	contents := []MCPContent{
		{Type: "text", Text: "请用小红书 App 在 " + deadline + " 前扫码登录创作者平台 👇"},
		{
			Type:     "image",
			MimeType: "image/png",
			Data:     creatorBase64,
		},
	}
	return &MCPToolResult{Content: contents}
}

// handleGetCreatorHome 获取创作者首页数据
func (s *AppServer) handleGetCreatorHome(ctx context.Context, botID string) *MCPToolResult {
	logrus.Infof("MCP [%s]: 获取创作者首页数据", botID)

	info, err := s.xiaohongshuService.GetCreatorHome(ctx, botID)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "获取创作者首页失败: " + err.Error()}},
			IsError: true,
		}
	}

	jsonData, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("序列化失败: %v", err)}},
			IsError: true,
		}
	}

	return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: string(jsonData)}}}
}

// handleListNotes 列出创作者后台笔记
func (s *AppServer) handleListNotes(ctx context.Context, botID string) *MCPToolResult {
	logrus.Infof("MCP [%s]: 列出笔记", botID)

	notes, err := s.xiaohongshuService.ListNotes(ctx, botID)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "获取笔记列表失败: " + err.Error()}},
			IsError: true,
		}
	}

	jsonData, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("序列化失败: %v", err)}},
			IsError: true,
		}
	}

	return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: string(jsonData)}}}
}

// handleManageNote 管理笔记（删除/置顶）
func (s *AppServer) handleManageNote(ctx context.Context, botID string, args ManageNoteArgs) *MCPToolResult {
	logrus.Infof("MCP: 管理笔记 - Feed ID: %s, Action: %s", args.FeedID, args.Action)

	if args.FeedID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "缺少 feed_id 参数"}},
			IsError: true,
		}
	}

	var err error
	switch args.Action {
	case "delete":
		err = s.xiaohongshuService.DeleteNote(ctx, botID, args.FeedID)
	case "pin":
		err = s.xiaohongshuService.PinNote(ctx, botID, args.FeedID)
	default:
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("不支持的操作: %s（支持 delete、pin）", args.Action)}},
			IsError: true,
		}
	}

	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("%s 失败: %v", args.Action, err)}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("笔记 %s %s 成功", args.FeedID, args.Action)}},
	}
}

// handlePublishLongform 发布长文
func (s *AppServer) handlePublishLongform(ctx context.Context, botID string, args PublishLongformArgs) *MCPToolResult {
	logrus.Infof("MCP: 发布长文 - 标题: %s", args.Title)

	err := s.xiaohongshuService.PublishLongform(ctx, botID, args.Title, args.Content, args.Desc, args.Tags, args.Visibility)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "长文发布失败: " + err.Error()}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("长文发布成功 - 标题: %s", args.Title)}},
	}
}

// handlePublishTextToImage 文字配图发布
// textContent 为原始文字，按空行拆分为多张卡片
// publishTextToImageArgs handler 内部参数
type publishTextToImageArgs struct {
	Title      string
	Content    string
	TextCards  []string
	ImageStyle string
	Tags       []string
	ScheduleAt string
	IsOriginal bool
	Visibility string
}

func (s *AppServer) handlePublishTextToImage(ctx context.Context, botID string, args publishTextToImageArgs, textContent string) *MCPToolResult {
	// 如果 TextCards 为空，从 textContent 按空行拆分
	textCards := args.TextCards
	if len(textCards) == 0 && textContent != "" {
		textCards = splitTextCards(textContent)
	}
	if len(textCards) == 0 {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "文字配图发布失败: text_image 不能为空"}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 文字配图发布 - 标题: %s, 卡片数: %d, 样式: %s", args.Title, len(textCards), args.ImageStyle)

	err := s.xiaohongshuService.PublishTextToImage(ctx, botID, args.Title, args.Content, textCards, args.ImageStyle, args.Tags, args.IsOriginal, args.Visibility, args.ScheduleAt)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "文字配图发布失败: " + err.Error()}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("文字配图发布成功 - 标题: %s, 卡片: %d张, 样式: %s", args.Title, len(textCards), args.ImageStyle)}},
	}
}

// splitTextCards 按空行拆分文字为多张卡片
func splitTextCards(text string) []string {
	// 按连续空行分割
	parts := strings.Split(text, "\n\n")
	var cards []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			cards = append(cards, trimmed)
		}
	}
	if len(cards) > 3 {
		cards = cards[:3]
	}
	return cards
}

// handleGetNotificationComments 获取通知评论列表
func (s *AppServer) handleGetNotificationComments(ctx context.Context, botID string) *MCPToolResult {
	logrus.Infof("MCP [%s]: 获取通知评论列表", botID)

	result, err := s.xiaohongshuService.GetNotificationComments(ctx, botID)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "获取通知评论失败: " + err.Error()}},
			IsError: true,
		}
	}

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("序列化失败: %v", err)}},
			IsError: true,
		}
	}

	return &MCPToolResult{Content: []MCPContent{{Type: "text", Text: string(jsonData)}}}
}

// handleReplyNotificationComment 通知页回复评论
func (s *AppServer) handleReplyNotificationComment(ctx context.Context, botID string, args ReplyNotificationArgs) *MCPToolResult {
	logrus.Infof("MCP: 通知页回复第 %d 条评论", args.CommentIndex)

	if args.Content == "" {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "缺少 content 参数"}},
			IsError: true,
		}
	}

	err := s.xiaohongshuService.ReplyNotificationComment(ctx, botID, args.CommentIndex, args.Content)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: "回复失败: " + err.Error()}},
			IsError: true,
		}
	}

	return &MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("通知页第 %d 条评论回复成功", args.CommentIndex)}},
	}
}

// handleReplyComment 处理回复评论
func (s *AppServer) handleReplyComment(ctx context.Context, botID string, args map[string]interface{}) *MCPToolResult {
	logrus.Infof("MCP [%s]: 回复评论", botID)

	// 解析参数
	feedID, ok := args["feed_id"].(string)
	if !ok || feedID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "回复评论失败: 缺少feed_id参数",
			}},
			IsError: true,
		}
	}

	xsecToken, ok := args["xsec_token"].(string)
	if !ok || xsecToken == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "回复评论失败: 缺少xsec_token参数",
			}},
			IsError: true,
		}
	}

	commentID, _ := args["comment_id"].(string)
	userID, _ := args["user_id"].(string)
	if commentID == "" && userID == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "回复评论失败: 缺少comment_id或user_id参数",
			}},
			IsError: true,
		}
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "回复评论失败: 缺少content参数",
			}},
			IsError: true,
		}
	}

	logrus.Infof("MCP: 回复评论 - Feed ID: %s, Comment ID: %s, User ID: %s, 内容长度: %d", feedID, commentID, userID, len(content))

	// 回复评论
	result, err := s.xiaohongshuService.ReplyCommentToFeed(ctx, botID, feedID, xsecToken, commentID, userID, content)
	if err != nil {
		return &MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: "回复评论失败: " + err.Error(),
			}},
			IsError: true,
		}
	}

	// 返回成功结果
	responseText := fmt.Sprintf("评论回复成功 - Feed ID: %s, Comment ID: %s, User ID: %s", result.FeedID, result.TargetCommentID, result.TargetUserID)
	return &MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: responseText,
		}},
	}
}
