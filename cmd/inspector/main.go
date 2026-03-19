// inspector — 小红书网页调试工具。
//
// 用法:
//
//	inspector dom      --profile bot10 --url URL --selectors "sel1,sel2,sel3"
//	inspector eval     --profile bot10 --url URL --js "JS expression"
//	inspector screenshot --profile bot10 --url URL [--output path.png]
//	inspector selectors --profile bot10 [--headful]
//	inspector dump-dom --profile bot10 --url URL [--selector "body"]
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/xpzouying/xiaohongshu-mcp/browser"
)

var profilesDir = getProfilesDir()

func getProfilesDir() string {
	if d := os.Getenv("XHS_PROFILES_DIR"); d != "" {
		return d
	}
	return "/home/rooot/.xhs-profiles"
}

// 所有 xiaohongshu-mcp 中使用的关键选择器，按页面分组。
var selectorSuites = []selectorSuite{
	{
		Name: "主站首页 (explore)",
		URL:  "https://www.xiaohongshu.com/explore",
		Selectors: []selectorCheck{
			{Sel: ".login-container", Desc: "登录弹窗（未登录时出现）"},
			{Sel: ".side-bar .user", Desc: "侧栏用户信息（已登录时出现）"},
			{Sel: ".login-container .qrcode-img", Desc: "主站二维码图片"},
			{Sel: "div#app", Desc: "主应用容器"},
			{Sel: "div.main-container li.user.side-bar-component a.link-wrapper span.channel", Desc: "用户主页链接"},
		},
	},
	{
		Name: "创作者平台登录页",
		URL:  "https://creator.xiaohongshu.com/login?source=official",
		Selectors: []selectorCheck{
			{Sel: ".css-wemwzq", Desc: "QR 码切换按钮"},
			{Sel: ".css-1lhmg90", Desc: "QR 码图片元素"},
		},
	},
	{
		Name: "创作者平台首页",
		URL:  "https://creator.xiaohongshu.com/creator/home?source=official",
		Selectors: []selectorCheck{
			{Sel: ".account-name", Desc: "账号名称"},
			{Sel: ".name-box", Desc: "名称容器（备用）"},
			{Sel: ".creator-block.default-cursor", Desc: "数据卡片"},
		},
	},
	{
		Name: "笔记管理页",
		URL:  "https://creator.xiaohongshu.com/new/note-manager?source=official",
		Selectors: []selectorCheck{
			{Sel: "[data-impression]", Desc: "笔记条目"},
		},
	},
	{
		Name: "发布页",
		URL:  "https://creator.xiaohongshu.com/publish/publish?source=official",
		Selectors: []selectorCheck{
			{Sel: "div.upload-content", Desc: "上传区域"},
			{Sel: "div.creator-tab", Desc: "创作者 Tab"},
			{Sel: "div.d-input input", Desc: "标题输入框"},
			{Sel: ".publish-page-publish-btn button.bg-red", Desc: "发布按钮"},
			{Sel: "#creator-editor-topic-container", Desc: "话题容器"},
		},
	},
}

type selectorSuite struct {
	Name      string
	URL       string
	Selectors []selectorCheck
}

type selectorCheck struct {
	Sel  string
	Desc string
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "dom":
		cmdDOM(args)
	case "eval":
		cmdEval(args)
	case "screenshot":
		cmdScreenshot(args)
	case "selectors":
		cmdSelectors(args)
	case "dump-dom":
		cmdDumpDOM(args)
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`inspector — 小红书网页调试工具

命令:
  dom        测试 CSS 选择器
  eval       执行 JavaScript
  screenshot 页面截图
  selectors  批量测试所有 MCP 预置选择器
  dump-dom   导出页面 DOM 结构
  help       显示此帮助

公共参数:
  --profile   账号 profile 名称 (如 bot10)
  --headful   使用有头模式（可见浏览器窗口）
  --timeout   超时秒数 (默认 30)`)
}

// --- 公共 helpers ---

type commonFlags struct {
	profile string
	headful bool
	timeout int
}

func parseCommon(fs *flag.FlagSet, args []string) commonFlags {
	var f commonFlags
	fs.StringVar(&f.profile, "profile", "bot10", "Chrome profile 名称")
	fs.BoolVar(&f.headful, "headful", false, "有头模式")
	fs.IntVar(&f.timeout, "timeout", 30, "超时秒数")
	fs.Parse(args)
	return f
}

func openBrowser(f commonFlags) (*browser.Browser, *rod.Page) {
	profileDir := filepath.Join(profilesDir, f.profile)
	headless := !f.headful
	b := browser.NewBrowser(headless, browser.WithProfileDir(profileDir))
	page := b.NewPage()
	return b, page
}

func navigateAndWait(page *rod.Page, url string, timeoutSec int) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	pp := page.Context(ctx)
	pp.MustNavigate(url).MustWaitLoad()
	time.Sleep(2 * time.Second)
}

// --- dom 命令 ---

func cmdDOM(args []string) {
	fs := flag.NewFlagSet("dom", flag.ExitOnError)
	var url, selectors string
	fs.StringVar(&url, "url", "", "目标 URL")
	fs.StringVar(&selectors, "selectors", "", "逗号分隔的 CSS 选择器")
	f := parseCommon(fs, args)

	if url == "" || selectors == "" {
		fmt.Fprintln(os.Stderr, "用法: inspector dom --profile bot10 --url URL --selectors 'sel1,sel2'")
		os.Exit(1)
	}

	b, page := openBrowser(f)
	defer b.Close()
	defer page.Close()

	navigateAndWait(page, url, f.timeout)

	sels := strings.Split(selectors, ",")
	for _, sel := range sels {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			continue
		}
		testSelector(page, sel)
	}
}

func testSelector(page *rod.Page, sel string) {
	js := fmt.Sprintf(`() => {
		const els = document.querySelectorAll(%q);
		if (els.length === 0) return JSON.stringify({count: 0, items: []});
		const items = [];
		els.forEach((el, i) => {
			if (i >= 5) return; // 最多返回5个
			items.push({
				tag: el.tagName.toLowerCase(),
				id: el.id || '',
				class: el.className || '',
				text: (el.textContent || '').trim().substring(0, 100),
				attrs: Object.fromEntries(
					Array.from(el.attributes)
						.filter(a => ['src','href','data-impression','placeholder','type','role'].includes(a.name))
						.map(a => [a.name, a.value.substring(0, 200)])
				),
			});
		});
		return JSON.stringify({count: els.length, items: items});
	}`, sel)

	res, err := page.Eval(js)
	if err != nil {
		fmt.Printf("❌ %s → 执行出错: %v\n", sel, err)
		return
	}

	fmt.Printf("\n=== %s ===\n%s\n", sel, formatJSON(res.Value.Str()))
}

func formatJSON(s string) string {
	// 简单格式化 JSON 输出
	s = strings.ReplaceAll(s, `"count":0`, `"count": 0 ❌ 未找到`)
	if strings.Contains(s, `"count": 0`) {
		return s
	}
	return strings.ReplaceAll(s, ",", ",\n  ")
}

// --- eval 命令 ---

func cmdEval(args []string) {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	var url, jsCode string
	fs.StringVar(&url, "url", "", "目标 URL")
	fs.StringVar(&jsCode, "js", "", "JavaScript 代码")
	f := parseCommon(fs, args)

	if url == "" || jsCode == "" {
		fmt.Fprintln(os.Stderr, "用法: inspector eval --profile bot10 --url URL --js 'code'")
		os.Exit(1)
	}

	b, page := openBrowser(f)
	defer b.Close()
	defer page.Close()

	navigateAndWait(page, url, f.timeout)

	// 包装为函数表达式
	wrappedJS := fmt.Sprintf("() => { return %s; }", jsCode)
	res, err := page.Eval(wrappedJS)
	if err != nil {
		// 如果包装失败，尝试直接执行
		wrappedJS = fmt.Sprintf("() => { %s }", jsCode)
		res, err = page.Eval(wrappedJS)
		if err != nil {
			fmt.Fprintf(os.Stderr, "JS 执行出错: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println(res.Value.String())
}

// --- screenshot 命令 ---

func cmdScreenshot(args []string) {
	fs := flag.NewFlagSet("screenshot", flag.ExitOnError)
	var url, output string
	var fullPage bool
	fs.StringVar(&url, "url", "", "目标 URL")
	fs.StringVar(&output, "output", "", "输出路径 (默认: /home/rooot/.openclaw/media/xhs-debug-{profile}.png)")
	fs.BoolVar(&fullPage, "full", false, "全页面截图")
	f := parseCommon(fs, args)

	if url == "" {
		fmt.Fprintln(os.Stderr, "用法: inspector screenshot --profile bot10 --url URL [--output path.png]")
		os.Exit(1)
	}
	if output == "" {
		output = fmt.Sprintf("/home/rooot/.openclaw/media/xhs-debug-%s.png", f.profile)
	}

	b, page := openBrowser(f)
	defer b.Close()
	defer page.Close()

	navigateAndWait(page, url, f.timeout)

	var data []byte
	if fullPage {
		data, _ = page.Screenshot(fullPage, &proto.PageCaptureScreenshot{
			Format: proto.PageCaptureScreenshotFormatPng,
		})
	} else {
		data, _ = page.Screenshot(false, &proto.PageCaptureScreenshot{
			Format: proto.PageCaptureScreenshotFormatPng,
		})
	}

	if len(data) == 0 {
		fmt.Fprintln(os.Stderr, "截图失败：未获取到数据")
		os.Exit(1)
	}

	os.MkdirAll(filepath.Dir(output), 0755)
	if err := os.WriteFile(output, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "保存截图失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("截图已保存: %s (%d bytes)\n", output, len(data))

	// 同时输出 base64（方便直接在终端查看）
	if len(data) < 500*1024 { // 小于 500KB 才输出 base64
		fmt.Printf("Base64 (data URI): data:image/png;base64,%s\n", base64.StdEncoding.EncodeToString(data)[:100]+"...")
	}
}

// --- selectors 命令 ---

func cmdSelectors(args []string) {
	fs := flag.NewFlagSet("selectors", flag.ExitOnError)
	f := parseCommon(fs, args)

	b, page := openBrowser(f)
	defer b.Close()
	defer page.Close()

	totalPass, totalFail := 0, 0

	for _, suite := range selectorSuites {
		fmt.Printf("\n========================================\n")
		fmt.Printf("📄 %s\n", suite.Name)
		fmt.Printf("🔗 %s\n", suite.URL)
		fmt.Printf("========================================\n")

		navigateAndWait(page, suite.URL, f.timeout)

		for _, sc := range suite.Selectors {
			js := fmt.Sprintf(`() => {
				const els = document.querySelectorAll(%q);
				return JSON.stringify({
					count: els.length,
					sample: els.length > 0 ? (els[0].textContent || '').trim().substring(0, 80) : '',
				});
			}`, sc.Sel)

			res, err := page.Eval(js)
			if err != nil {
				fmt.Printf("  ❌ %-50s  %s  (执行出错: %v)\n", sc.Sel, sc.Desc, err)
				totalFail++
				continue
			}

			v := res.Value.Str()
			if strings.Contains(v, `"count":0`) {
				fmt.Printf("  ❌ %-50s  %s  (未找到)\n", sc.Sel, sc.Desc)
				totalFail++
			} else {
				fmt.Printf("  ✅ %-50s  %s  %s\n", sc.Sel, sc.Desc, v)
				totalPass++
			}
		}
	}

	fmt.Printf("\n========================================\n")
	fmt.Printf("汇总: ✅ %d 通过  ❌ %d 失败  共 %d 项\n", totalPass, totalFail, totalPass+totalFail)
	fmt.Printf("========================================\n")

	if totalFail > 0 {
		os.Exit(1)
	}
}

// --- dump-dom 命令 ---

func cmdDumpDOM(args []string) {
	fs := flag.NewFlagSet("dump-dom", flag.ExitOnError)
	var url, selector string
	var depth int
	fs.StringVar(&url, "url", "", "目标 URL")
	fs.StringVar(&selector, "selector", "body", "根选择器")
	fs.IntVar(&depth, "depth", 3, "遍历深度")
	f := parseCommon(fs, args)

	if url == "" {
		fmt.Fprintln(os.Stderr, "用法: inspector dump-dom --profile bot10 --url URL [--selector body] [--depth 3]")
		os.Exit(1)
	}

	b, page := openBrowser(f)
	defer b.Close()
	defer page.Close()

	navigateAndWait(page, url, f.timeout)

	js := fmt.Sprintf(`() => {
		function dump(el, depth, maxDepth, indent) {
			if (depth > maxDepth || !el) return '';
			let tag = el.tagName ? el.tagName.toLowerCase() : '#text';
			let attrs = '';
			if (el.attributes) {
				for (let a of el.attributes) {
					if (['id','class','data-impression','role','href','src','placeholder','type','contenteditable'].includes(a.name)) {
						let v = a.value.substring(0, 80);
						attrs += ' ' + a.name + '="' + v + '"';
					}
				}
			}
			let line = indent + '<' + tag + attrs + '>';
			let text = '';
			if (el.childNodes) {
				for (let c of el.childNodes) {
					if (c.nodeType === 3) {
						let t = c.textContent.trim();
						if (t) text += t.substring(0, 50);
					}
				}
			}
			if (text) line += ' ' + text;
			let result = line + '\n';
			if (el.children && depth < maxDepth) {
				for (let c of el.children) {
					result += dump(c, depth + 1, maxDepth, indent + '  ');
				}
			}
			return result;
		}
		const root = document.querySelector(%q);
		if (!root) return '未找到: %s';
		return dump(root, 0, %d, '');
	}`, selector, selector, depth)

	res, err := page.Eval(js)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DOM 导出出错: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(res.Value.Str())
}
