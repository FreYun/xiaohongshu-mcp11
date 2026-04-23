package xiaohongshu

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sirupsen/logrus"
)

// humanSleep 模拟人类操作延迟：在 base 时长基础上添加 ±30% 的随机抖动。
// 例如 humanSleep(2 * time.Second) 实际等待 1.4s ~ 2.6s。
func humanSleep(base time.Duration) {
	if base <= 0 {
		return
	}
	jitterPct := 0.3
	jitterRange := float64(base) * jitterPct * 2
	jitter := rand.Float64()*jitterRange - float64(base)*jitterPct
	actual := time.Duration(float64(base) + jitter)
	if actual < 50*time.Millisecond {
		actual = 50 * time.Millisecond
	}
	time.Sleep(actual)
}

// humanCharDelay returns a per-character typing delay for loops that type one
// rune at a time via elem.Input(string(char)). Mix: 70% normal (40-130ms),
// 20% fast burst (20-50ms), 10% thinking pause (200-600ms). Replaces the
// telltale uniform 50ms cadence of the old tag-input loop.
func humanCharDelay() time.Duration {
	r := rand.Intn(100)
	switch {
	case r < 70:
		return time.Duration(40+rand.Intn(90)) * time.Millisecond
	case r < 90:
		return time.Duration(20+rand.Intn(30)) * time.Millisecond
	default:
		return time.Duration(200+rand.Intn(400)) * time.Millisecond
	}
}

// humanType types text into a contenteditable/input element one rune at a
// time, with realistic per-character delays from humanCharDelay(). Matches
// the per-char pattern already used by publish.go's tag input, which keeps
// keydown/keyup density in the range real users produce.
// NOTE: for rich editors that require real keyboard events (e.g. tag
// popovers), publish.go still uses its own CDP path. This helper is for
// comment boxes and plain contenteditable fields where elem.Input works.
func humanType(elem *rod.Element, text string) error {
	for _, char := range text {
		if err := elem.Input(string(char)); err != nil {
			return err
		}
		time.Sleep(humanCharDelay())
	}
	return nil
}

// humanSleepRange sleeps a uniformly-random duration in [minMs, maxMs] ms.
// Use for pacing between discrete actions (post-navigation reading pause,
// pre-submit hesitation, inter-action gap in batch operations).
func humanSleepRange(minMs, maxMs int) {
	if maxMs < minMs {
		minMs, maxMs = maxMs, minMs
	}
	span := maxMs - minMs
	if span <= 0 {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	d := time.Duration(minMs+rand.Intn(span+1)) * time.Millisecond
	time.Sleep(d)
}

// navigateToFeedDetail 模拟真实用户进入帖子详情页：
// 1. 确保在小红书页面上（搜索结果/explore），不在则先导航到 explore
// 2. 在当前页面查找目标帖子卡片，找到则 humanClick 点击（像真人一样）
// 3. 找不到卡片时 fallback 到 JS 跳转（仍从当前页面跳，保留自然 Referer）
func navigateToFeedDetail(page *rod.Page, feedID, xsecToken string) error {
	info, infoErr := page.Info()
	onXHS := infoErr == nil && info != nil && strings.Contains(info.URL, "xiaohongshu.com")

	if !onXHS {
		logrus.Info("navigateToFeedDetail: 不在小红书页面，先导航到 explore")
		if err := page.Navigate("https://www.xiaohongshu.com/explore"); err != nil {
			return fmt.Errorf("navigate to explore failed: %w", err)
		}
		if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
			logrus.Warnf("explore WaitDOMStable: %v", err)
		}
		humanSleepRange(2000, 4000)
	} else {
		humanSleepRange(800, 1500)
	}

	// 尝试在当前页面找到目标帖子卡片并 humanClick。
	// 用 page.Eval（而非 elem.Eval）获取坐标，避免 element execution context stale 导致挂起。
	cardSelector := fmt.Sprintf(`a[href*="/explore/%s"]`, feedID)
	rectResult, evalErr := page.Timeout(8*time.Second).Eval(`(sel) => {
		const link = document.querySelector(sel);
		if (!link) return null;
		// 先 scrollIntoView 触发懒加载渲染（headless 下卡片未渲染时 rect 全为 0）
		link.scrollIntoView({block: 'center'});
		return new Promise(resolve => setTimeout(() => {
			// 渲染完成后 parent-walk 找有尺寸的卡片容器
			let el = link;
			for (let i = 0; i < 6; i++) {
				const r = el.getBoundingClientRect();
				if (r.width > 20 && r.height > 20) {
					resolve({x: r.x, y: r.y, w: r.width, h: r.height});
					return;
				}
				if (el.parentElement) el = el.parentElement;
				else break;
			}
			resolve(null);
		}, 500));
	}`, cardSelector)

	if evalErr == nil && rectResult != nil && rectResult.Value.Str() != "null" {
		m := rectResult.Value.Map()
		w := m["w"].Num()
		h := m["h"].Num()
		if w > 0 && h > 0 {
			logrus.Infof("navigateToFeedDetail: 找到卡片 %s (%.0fx%.0f)，模拟鼠标点击", feedID, w, h)
			humanSleepRange(300, 600)
			x := m["x"].Num() + w*(0.3+rand.Float64()*0.4)
			y := m["y"].Num() + h*(0.3+rand.Float64()*0.4)
			steps := 6 + rand.Intn(8)
			if moveErr := page.Mouse.MoveLinear(proto.Point{X: x, Y: y}, steps); moveErr == nil {
				time.Sleep(time.Duration(40+rand.Intn(80)) * time.Millisecond)
				if clickErr := page.Mouse.Click(proto.InputMouseButtonLeft, 1); clickErr == nil {
					humanSleepRange(2000, 4000)
					if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
						logrus.Warnf("detail WaitDOMStable: %v", err)
					}
					return checkPageAccessible(page)
				} else {
					logrus.Warnf("navigateToFeedDetail: mouse click 失败: %v, fallback JS", clickErr)
				}
			} else {
				logrus.Warnf("navigateToFeedDetail: mouse move 失败: %v, fallback JS", moveErr)
			}
		} else {
			logrus.Infof("navigateToFeedDetail: 卡片 %s 尺寸为0，fallback JS", feedID)
		}
	} else {
		logrus.Infof("navigateToFeedDetail: 未找到卡片 %s (err=%v)，fallback JS", feedID, evalErr)
	}

	// fallback: JS 跳转
	detailURL := makeFeedDetailURL(feedID, xsecToken)
	logrus.Infof("navigateToFeedDetail: JS 跳转到详情页 %s", feedID)
	if _, err := page.Eval(`(url) => { window.location.href = url; }`, detailURL); err != nil {
		return fmt.Errorf("JS navigate to detail failed: %w", err)
	}
	if err := page.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("detail WaitDOMStable: %v", err)
	}
	humanSleepRange(2000, 4000)

	return checkPageAccessible(page)
}

// closeDetailAndReturn 操作完详情页后回到 feed 列表页。
// 用 history.back() 回退（比 Escape 可靠，XHS 弹窗不响应 Escape）。
// 如果 back 后仍在详情页，fallback 导航到 explore。
func closeDetailAndReturn(page *rod.Page) {
	humanSleepRange(500, 1000)

	page.Eval(`() => history.back()`)
	humanSleepRange(1000, 2000)

	// 检查是否回到了列表页（URL 不含 /explore/ + feedID 格式）
	info, err := page.Info()
	if err == nil && info != nil {
		// 详情页 URL 格式: /explore/{24位hex}
		if strings.Contains(info.URL, "/explore/") && len(info.URL) > 60 {
			logrus.Warn("closeDetailAndReturn: history.back 未生效，导航回 explore")
			page.Navigate("https://www.xiaohongshu.com/explore")
			page.WaitDOMStable(time.Second, 0.1)
			humanSleepRange(1500, 3000)
			return
		}
	}

	logrus.Info("closeDetailAndReturn: 已回到列表页")
}

// humanClick moves the mouse to a jittered point inside the element via
// MoveLinear (generates intermediate mousemove events), pauses briefly to
// simulate hover, then clicks. Much closer to a real cursor than elem.Click()
// which dispatches a click event without moving the OS-level cursor.
// Falls back to elem.Click() if the bounding rect is unavailable or movement
// fails — behavior stays correct even if stealth pieces fail.
func humanClick(page *rod.Page, elem *rod.Element) error {
	rectVal, err := elem.Eval(`() => {
		const r = this.getBoundingClientRect();
		return {x: r.x, y: r.y, w: r.width, h: r.height};
	}`)
	if err != nil || rectVal == nil {
		return elem.Click(proto.InputMouseButtonLeft, 1)
	}
	m := rectVal.Value.Map()
	w := m["w"].Num()
	h := m["h"].Num()
	if w == 0 || h == 0 {
		return elem.Click(proto.InputMouseButtonLeft, 1)
	}
	// Aim for the center 40% so we don't land on a pixel-exact corner.
	x := m["x"].Num() + w*(0.3+rand.Float64()*0.4)
	y := m["y"].Num() + h*(0.3+rand.Float64()*0.4)

	steps := 6 + rand.Intn(8)
	if err := page.Mouse.MoveLinear(proto.Point{X: x, Y: y}, steps); err != nil {
		return elem.Click(proto.InputMouseButtonLeft, 1)
	}
	time.Sleep(time.Duration(40+rand.Intn(80)) * time.Millisecond)
	return page.Mouse.Click(proto.InputMouseButtonLeft, 1)
}
