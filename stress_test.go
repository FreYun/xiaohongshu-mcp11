package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

const (
	mcpEndpoint = "http://localhost:18060"
	callTimeout = 2 * time.Minute
)

// ---------- system snapshot ----------

type snapshot struct {
	chromeProcs int // Chrome 主进程 + 子进程总数
	rodProcs    int // Rod (xhs-profiles) 相关 Chrome 进程
	tcpConns    int // 总 TCP 连接数
	chromeConns int // Chrome TCP 连接数
	memUsedMB   int // 系统已用内存 MB
}

func takeSnapshot(t *testing.T) snapshot {
	t.Helper()
	s := snapshot{}

	// Chrome process count
	out, _ := exec.Command("bash", "-c", "ps aux | grep -c '[c]hrome'").Output()
	s.chromeProcs, _ = strconv.Atoi(strings.TrimSpace(string(out)))

	// Rod (xhs-profiles) Chrome count
	out, _ = exec.Command("bash", "-c", "ps aux | grep '[c]hrome' | grep -c 'xhs-profiles'").Output()
	s.rodProcs, _ = strconv.Atoi(strings.TrimSpace(string(out)))

	// TCP connections total
	out, _ = exec.Command("bash", "-c", "ss -tn | tail -n +2 | wc -l").Output()
	s.tcpConns, _ = strconv.Atoi(strings.TrimSpace(string(out)))

	// Chrome TCP connections
	out, _ = exec.Command("bash", "-c", "ss -tnp | grep -c chrome").Output()
	s.chromeConns, _ = strconv.Atoi(strings.TrimSpace(string(out)))

	// Memory used MB
	out, _ = exec.Command("bash", "-c", "free -m | awk '/^Mem:/{print $3}'").Output()
	s.memUsedMB, _ = strconv.Atoi(strings.TrimSpace(string(out)))

	return s
}

func (s snapshot) String() string {
	return fmt.Sprintf("chrome_procs=%d  rod_procs=%d  tcp_conns=%d  chrome_conns=%d  mem=%dMB",
		s.chromeProcs, s.rodProcs, s.tcpConns, s.chromeConns, s.memUsedMB)
}

func diffSnapshot(before, after snapshot) string {
	d := func(label string, b, a int) string {
		delta := a - b
		sign := "+"
		if delta < 0 {
			sign = ""
		}
		return fmt.Sprintf("%s: %d→%d (%s%d)", label, b, a, sign, delta)
	}
	return strings.Join([]string{
		d("chrome_procs", before.chromeProcs, after.chromeProcs),
		d("rod_procs", before.rodProcs, after.rodProcs),
		d("tcp_conns", before.tcpConns, after.tcpConns),
		d("chrome_conns", before.chromeConns, after.chromeConns),
		d("mem_MB", before.memUsedMB, after.memUsedMB),
	}, "  |  ")
}

// ---------- MCP helpers ----------

func connectBot(t *testing.T, botID string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "stress-test",
		Version: "1.0.0",
	}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   mcpEndpoint + "/mcp/" + botID,
		HTTPClient: &http.Client{Timeout: 0},
		MaxRetries: 20,
	}, nil)
	require.NoError(t, err, "failed to connect to bot %s", botID)
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) (text string, isErr bool, dur time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	start := time.Now()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      tool,
		Arguments: args,
	})
	dur = time.Since(start)

	if err != nil {
		return err.Error(), true, dur
	}
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, " | "), result.IsError, dur
}

func ensureHealthy(t *testing.T) {
	t.Helper()
	resp, err := http.Get(mcpEndpoint + "/health")
	require.NoError(t, err, "MCP service unreachable")
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
}

// ---------- The incremental test ----------

// TestStress_Incremental 递增式压测：
//
//	Step 0: 空闲基线快照
//	Step 1: 1 个 bot 调用 list_feeds，操作期间快照，完成后快照
//	Step 2: 2 个 bot 同时调用，操作期间快照，完成后快照
//	Step 3: 3 个 bot 同时调用（== semaphore 上限），操作期间快照，完成后快照
//
// 每步之间等 Chrome 完全退出，确保 delta 干净。
func TestStress_Incremental(t *testing.T) {
	ensureHealthy(t)

	bots := []string{"bot1", "bot2", "bot3"}

	// Step 0: idle baseline
	baseline := takeSnapshot(t)
	t.Logf("=== Step 0: IDLE baseline ===")
	t.Logf("  %s", baseline)

	for step := 1; step <= 3; step++ {
		activeBots := bots[:step]
		t.Logf("")
		t.Logf("=== Step %d: %d bot(s) concurrent [%s] ===", step, step, strings.Join(activeBots, ", "))

		before := takeSnapshot(t)
		t.Logf("  BEFORE: %s", before)

		// Launch concurrent calls
		type callResult struct {
			botID string
			text  string
			isErr bool
			dur   time.Duration
		}
		ch := make(chan callResult, step)

		for _, bot := range activeBots {
			go func(botID string) {
				session := connectBot(t, botID)
				defer session.Close()
				text, isErr, dur := callTool(t, session, "list_feeds", map[string]any{})
				ch <- callResult{botID, text, isErr, dur}
			}(bot)
		}

		// Wait a few seconds for Chrome to spin up, then snapshot "during"
		time.Sleep(5 * time.Second)
		during := takeSnapshot(t)
		t.Logf("  DURING (5s in): %s", during)
		t.Logf("    delta vs before: %s", diffSnapshot(before, during))

		// Collect results
		for i := 0; i < step; i++ {
			r := <-ch
			status := "OK"
			if r.isErr {
				status = "ERR"
			}
			// truncate text for readability
			text := r.text
			if len(text) > 80 {
				text = text[:80] + "..."
			}
			t.Logf("  RESULT %s: %s dur=%s text=%s", r.botID, status, r.dur.Round(time.Millisecond), text)
		}

		// Wait for Chrome to fully exit
		t.Log("  waiting for Chrome cleanup...")
		time.Sleep(8 * time.Second)

		after := takeSnapshot(t)
		t.Logf("  AFTER (cleanup): %s", after)
		t.Logf("    delta vs before: %s", diffSnapshot(before, after))
		t.Logf("    delta vs baseline: %s", diffSnapshot(baseline, after))

		ensureHealthy(t)
	}
}

// ---------- Health check concurrency (lightweight, no Chrome) ----------

func TestStress_HealthConcurrency(t *testing.T) {
	const n = 50
	ch := make(chan bool, n)

	start := time.Now()
	for i := 0; i < n; i++ {
		go func() {
			resp, err := http.Get(mcpEndpoint + "/health")
			if err != nil {
				ch <- false
				return
			}
			defer resp.Body.Close()
			io.ReadAll(resp.Body)
			ch <- resp.StatusCode == 200
		}()
	}

	ok := 0
	for i := 0; i < n; i++ {
		if <-ch {
			ok++
		}
	}
	dur := time.Since(start)
	t.Logf("Health: %d/%d OK in %s", ok, n, dur.Round(time.Millisecond))
	require.Equal(t, n, ok, "all health checks should pass")
}

// ---------- Circuit breaker (uses fake bot, serial) ----------

func TestStress_CircuitBreaker(t *testing.T) {
	ensureHealthy(t)
	fakeBotID := "bot_stress_test"

	t.Log("Phase 1: trigger 3 consecutive failures...")
	for i := 0; i < 3; i++ {
		session := connectBot(t, fakeBotID)
		text, isErr, dur := callTool(t, session, "list_feeds", map[string]any{})
		session.Close()
		if len(text) > 100 {
			text = text[:100] + "..."
		}
		t.Logf("  attempt %d: isErr=%v dur=%s text=%s", i+1, isErr, dur.Round(time.Millisecond), text)
		// Small pause between attempts to avoid semaphore contention
		time.Sleep(2 * time.Second)
	}

	t.Log("Phase 2: 4th request — expect circuit breaker fast-fail...")
	session := connectBot(t, fakeBotID)
	start := time.Now()
	text, isErr, dur := callTool(t, session, "list_feeds", map[string]any{})
	session.Close()
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	t.Logf("  4th attempt: isErr=%v dur=%s text=%s", isErr, dur.Round(time.Millisecond), text)

	elapsed := time.Since(start)
	if elapsed < 10*time.Second {
		t.Logf("  FAST FAIL confirmed (%s < 10s) — circuit breaker working", elapsed.Round(time.Millisecond))
	} else {
		t.Logf("  SLOW response (%s) — circuit breaker may not have triggered", elapsed.Round(time.Millisecond))
	}

	if isErr && (strings.Contains(text, "熔断") || strings.Contains(text, "circuit")) {
		t.Log("  Circuit breaker message found in response")
	}
}
