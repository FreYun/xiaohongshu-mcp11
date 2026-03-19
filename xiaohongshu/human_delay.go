package xiaohongshu

import (
	"math/rand"
	"time"
)

// humanSleep 模拟人类操作延迟：在 base 时长基础上添加 ±30% 的随机抖动。
// 例如 humanSleep(2 * time.Second) 实际等待 1.4s ~ 2.6s。
func humanSleep(base time.Duration) {
	if base <= 0 {
		return
	}
	// jitter 范围: -30% ~ +30%
	jitterPct := 0.3
	jitterRange := float64(base) * jitterPct * 2 // 总范围 60%
	jitter := rand.Float64()*jitterRange - float64(base)*jitterPct
	actual := time.Duration(float64(base) + jitter)
	if actual < 50*time.Millisecond {
		actual = 50 * time.Millisecond
	}
	time.Sleep(actual)
}
