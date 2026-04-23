package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/browser"
)

const (
	poolMaxAlive     = 3
	poolIdleTimeout  = 10 * time.Minute
	poolReapInterval = 60 * time.Second
	poolAcquireTimeout = 30 * time.Second
)

type poolEntry struct {
	browser    *browser.Browser
	lastActive time.Time
	botID      string
}

type BrowserPool struct {
	mu          sync.Mutex
	entries     map[string]*poolEntry
	maxAlive    int
	idleTimeout time.Duration
	stopReaper  chan struct{}
}

func NewBrowserPool() *BrowserPool {
	p := &BrowserPool{
		entries:     make(map[string]*poolEntry),
		maxAlive:    poolMaxAlive,
		idleTimeout: poolIdleTimeout,
		stopReaper:  make(chan struct{}),
	}
	go p.reaper()
	return p
}

func (p *BrowserPool) Acquire(botID string) (*browser.Browser, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.entries[botID]; ok {
		if entry.browser.IsAlive() {
			entry.lastActive = time.Now()
			logrus.Infof("BrowserPool: 复用已有浏览器 %s", botID)
			return entry.browser, nil
		}
		logrus.Warnf("BrowserPool: %s 的浏览器已死亡，重新创建", botID)
		entry.browser.Close()
		delete(p.entries, botID)
	}

	if len(p.entries) >= p.maxAlive {
		p.evictLRU()
	}

	logrus.Infof("BrowserPool: 为 %s 创建新浏览器", botID)
	b := newBrowserForBot(botID)
	p.entries[botID] = &poolEntry{
		browser:    b,
		lastActive: time.Now(),
		botID:      botID,
	}
	return b, nil
}

func (p *BrowserPool) Release(botID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.entries[botID]; ok {
		entry.lastActive = time.Now()
	}
}

func (p *BrowserPool) Evict(botID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.entries[botID]; ok {
		logrus.Infof("BrowserPool: 驱逐浏览器 %s", botID)
		go entry.browser.Close()
		delete(p.entries, botID)
	}
}

func (p *BrowserPool) evictLRU() {
	var oldest *poolEntry
	for _, entry := range p.entries {
		if oldest == nil || entry.lastActive.Before(oldest.lastActive) {
			oldest = entry
		}
	}
	if oldest != nil {
		logrus.Infof("BrowserPool: 池已满，LRU 驱逐 %s (空闲 %s)",
			oldest.botID, time.Since(oldest.lastActive).Round(time.Second))
		go oldest.browser.Close()
		delete(p.entries, oldest.botID)
	}
}

func (p *BrowserPool) reaper() {
	ticker := time.NewTicker(poolReapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopReaper:
			return
		case <-ticker.C:
			p.reapIdle()
		}
	}
}

func (p *BrowserPool) reapIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for botID, entry := range p.entries {
		if now.Sub(entry.lastActive) > p.idleTimeout {
			logrus.Infof("BrowserPool: 空闲超时回收 %s (空闲 %s)",
				botID, now.Sub(entry.lastActive).Round(time.Second))
			go entry.browser.Close()
			delete(p.entries, botID)
		}
	}
}

func (p *BrowserPool) CloseAll() {
	close(p.stopReaper)

	p.mu.Lock()
	defer p.mu.Unlock()

	for botID, entry := range p.entries {
		logrus.Infof("BrowserPool: 关闭 %s", botID)
		entry.browser.Close()
		delete(p.entries, botID)
	}
}

func (p *BrowserPool) Stats() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	alive := 0
	for _, entry := range p.entries {
		if entry.browser.IsAlive() {
			alive++
		}
	}
	return fmt.Sprintf("pool: %d entries, %d alive, max %d", len(p.entries), alive, p.maxAlive)
}
