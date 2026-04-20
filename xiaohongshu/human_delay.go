package xiaohongshu

import (
	"math/rand"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
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
