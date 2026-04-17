// 递增式并发压测工具
// 用法: go run ./cmd/stress -bots 1    （1 个 bot）
//       go run ./cmd/stress -bots 2    （2 个 bot 同时）
//       go run ./cmd/stress -bots 3    （3 个 bot 同时）
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func snapshot() map[string]int {
	m := map[string]int{}
	run := func(key, cmd string) {
		out, _ := exec.Command("bash", "-c", cmd).Output()
		m[key], _ = strconv.Atoi(strings.TrimSpace(string(out)))
	}
	run("chrome_procs", "ps aux | grep -c '[c]hrome'")
	run("rod_procs", "ps aux | grep '[r]od/browser' | wc -l")
	run("tcp_conns", "ss -tn | tail -n +2 | wc -l")
	run("chrome_conns", "ss -tnp 2>/dev/null | grep -c chrome")
	run("fd", "cat /proc/sys/fs/file-nr | awk '{print $1}'")
	run("mem_mb", "free -m | awk '/^Mem:/{print $3}'")
	return m
}

func printSnap(label string, s map[string]int) {
	fmt.Printf("  [%s] chrome=%d  rod=%d  tcp=%d  chrome_tcp=%d  fd=%d  mem=%dMB\n",
		label, s["chrome_procs"], s["rod_procs"], s["tcp_conns"], s["chrome_conns"], s["fd"], s["mem_mb"])
}

func printDelta(before, after map[string]int) {
	keys := []string{"chrome_procs", "rod_procs", "tcp_conns", "chrome_conns", "fd", "mem_mb"}
	parts := []string{}
	for _, k := range keys {
		d := after[k] - before[k]
		sign := "+"
		if d < 0 {
			sign = ""
		}
		parts = append(parts, fmt.Sprintf("%s:%s%d", k, sign, d))
	}
	fmt.Printf("  [DELTA] %s\n", strings.Join(parts, "  "))
}

func main() {
	n := flag.Int("bots", 1, "number of concurrent bots")
	flag.Parse()

	botIDs := []string{"bot1", "bot2", "bot3", "bot4", "bot5", "bot6", "bot7", "bot11"}
	if *n > len(botIDs) {
		*n = len(botIDs)
	}
	active := botIDs[:*n]

	fmt.Printf("=== 并发压测: %d bot(s) [%s] ===\n\n", *n, strings.Join(active, ", "))

	// BEFORE
	before := snapshot()
	printSnap("BEFORE", before)

	// 并发调用
	var wg sync.WaitGroup
	type result struct {
		bot  string
		ok   bool
		dur  time.Duration
		text string
	}
	ch := make(chan result, *n)

	fmt.Printf("\n  启动 %d 个并发 list_feeds...\n", *n)
	start := time.Now()

	for _, bot := range active {
		wg.Add(1)
		go func(botID string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			client := mcp.NewClient(&mcp.Implementation{Name: "stress", Version: "1.0"}, nil)
			session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
				Endpoint:   "http://localhost:18060/mcp/" + botID,
				HTTPClient: &http.Client{Timeout: 0},
				MaxRetries: 20,
			}, nil)
			if err != nil {
				ch <- result{botID, false, time.Since(start), "connect: " + err.Error()}
				return
			}
			defer session.Close()

			t0 := time.Now()
			res, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "list_feeds",
				Arguments: map[string]any{},
			})
			dur := time.Since(t0)

			if err != nil {
				ch <- result{botID, false, dur, err.Error()}
				return
			}
			var text string
			for _, c := range res.Content {
				if tc, ok := c.(*mcp.TextContent); ok {
					text = tc.Text
					break
				}
			}
			if len(text) > 60 {
				text = text[:60] + "..."
			}
			ch <- result{botID, !res.IsError, dur, text}
		}(bot)
	}

	// 5 秒后拍快照
	time.Sleep(5 * time.Second)
	during := snapshot()
	fmt.Println()
	printSnap("DURING 5s", during)
	printDelta(before, during)

	// 等全部完成
	wg.Wait()
	close(ch)

	fmt.Printf("\n  === 结果 ===\n")
	for r := range ch {
		status := "OK"
		if !r.ok {
			status = "ERR"
		}
		fmt.Printf("  %s: %s  dur=%s  %s\n", r.bot, status, r.dur.Round(time.Millisecond), r.text)
	}
	totalDur := time.Since(start)
	fmt.Printf("  总耗时: %s\n", totalDur.Round(time.Millisecond))

	// 等 Chrome 退出
	fmt.Println("\n  等待 Chrome 退出...")
	time.Sleep(12 * time.Second)
	after := snapshot()
	printSnap("AFTER", after)
	printDelta(before, after)
	fmt.Println()
}
