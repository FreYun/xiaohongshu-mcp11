# xiaohongshu-mcp

小红书 MCP (Model Context Protocol) 服务，通过浏览器自动化实现小红书的完整操作能力。支持多 bot 实例并行运行。

## 功能概览

### 登录认证
| 工具 | 说明 |
|------|------|
| `check_login_status` | 检查主站 + 创作者平台登录状态 |
| `get_login_qrcode` | 获取主站登录二维码 |
| `get_creator_login_qrcode` | 获取创作者平台登录二维码 |
| `get_both_login_qrcodes` | 同时获取两个平台二维码 |
| `delete_cookies` | 清除所有登录状态（cookies + 浏览器内部存储） |

### 内容发布
| 工具 | 说明 |
|------|------|
| `publish_content` | 发布图文（支持图片上传 + 文字配图两种模式） |
| `publish_longform` | 发布长文（自动排版） |
| `publish_with_video` | 发布视频 |

### 浏览与搜索
| 工具 | 说明 |
|------|------|
| `list_feeds` | 获取首页 Feed 列表 |
| `search_feeds` | 关键词搜索（支持排序、类型、时间等筛选） |
| `get_feed_detail` | 获取笔记详情（含评论） |
| `user_profile` | 获取用户主页信息 |

### 互动操作
| 工具 | 说明 |
|------|------|
| `post_comment_to_feed` | 发表评论 |
| `reply_comment_in_feed` | 回复评论 |
| `like_feed` | 点赞 / 取消点赞 |
| `favorite_feed` | 收藏 / 取消收藏 |

### 创作者管理
| 工具 | 说明 |
|------|------|
| `get_creator_home` | 获取创作者后台数据（粉丝、获赞等） |
| `list_notes` | 获取笔记列表（含浏览/点赞/收藏数据） |
| `manage_note` | 笔记管理（删除、置顶） |
| `get_notification_comments` | 获取通知评论列表 |
| `reply_notification_comment` | 在通知页回复评论 |

## 文字配图发布

通过 `publish_content` 的 `text_to_image` 模式，自动将文字生成卡片图片并发布：

```bash
publish_content(
  title: "标题",
  content: "图下正文",
  text_to_image: true,
  text_content: "第一张卡片内容\n\n第二张卡片内容",  # 空行分隔多张卡片，最多3张
  image_style: "基础",  # 可选：基础、插图、光影、弥散、涂写、手写、备忘、便签、边框
  tags: ["标签1", "标签2"],
  visibility: "仅自己可见"
)
```

## 快速开始

### 编译

```bash
go build -o xiaohongshu-mcp .
```

### 单实例启动

```bash
# 有头模式（可看到浏览器界面）
DISPLAY=:99 ./xiaohongshu-mcp --headless=false -port=:18070

# 无头模式
./xiaohongshu-mcp -port=:18060
```

### 多 bot 启动

```bash
# 启动 bot1-bot10（端口 18061-18070）
bash start-all.sh
```

每个 bot 映射：`bot1 → :18061, bot2 → :18062, ... bot10 → :18070`

如果 `~/.openclaw/browser/botN/user-data` 存在，自动使用 Chrome profile 持久化模式。

### 健康检查

```bash
curl http://localhost:18070/health
```

## 浏览器模式

| 模式 | 说明 | Cookie 来源 |
|------|------|-------------|
| Cookie 模式（默认） | 临时浏览器 profile | `cookies.json` 文件 |
| Profile 模式 | 持久化 Chrome profile | Chrome 内部存储 + `cookies.json` 注入 |

Profile 模式通过 `-profile-dir` 参数启用：

```bash
./xiaohongshu-mcp --headless=false -port=:18070 -profile-dir=/path/to/user-data
```

## API

同时提供 MCP (SSE) 和 HTTP REST 两种接口：

- **MCP**: `POST /mcp`（需 initialize → tools/call）
- **HTTP**: `GET/POST /api/v1/...`

详见 [API 文档](docs/API.md)。

## 技术栈

- Go + [go-rod](https://github.com/go-rod/rod) 浏览器自动化
- [stealth](https://github.com/nickolasclarke/stealth) 反检测
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) 协议实现
- [Gin](https://github.com/gin-gonic/gin) HTTP 框架
