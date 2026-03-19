package test_manage

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// NotifyLoginSuccessTo 向指定 callbackURL 发送登录成功通知
// 如果 callbackURL 为空则不发送
func NotifyLoginSuccessTo(callbackURL, accountID, loginType string) {
	if callbackURL == "" {
		return
	}

	type loginSuccessEvent struct {
		Event     string `json:"event"`
		AccountID string `json:"account_id"`
		LoginType string `json:"login_type"`
		Timestamp string `json:"timestamp"`
	}

	event := loginSuccessEvent{
		Event:     "login_success",
		AccountID: accountID,
		LoginType: loginType,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	body, err := json.Marshal(event)
	if err != nil {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(callbackURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

// TestNotifyLoginSuccessTo_WithURL 验证传入 callbackURL 时正确 POST 事件
func TestNotifyLoginSuccessTo_WithURL(t *testing.T) {
	type loginEvent struct {
		Event     string `json:"event"`
		AccountID string `json:"account_id"`
		LoginType string `json:"login_type"`
		Timestamp string `json:"timestamp"`
	}

	received := make(chan loginEvent, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST 请求, 实际 %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取请求体失败: %v", err)
			return
		}
		defer r.Body.Close()

		var evt loginEvent
		if err := json.Unmarshal(body, &evt); err != nil {
			t.Errorf("解析JSON失败: %v", err)
			return
		}
		received <- evt
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 直接传入 URL，不依赖全局配置
	NotifyLoginSuccessTo(srv.URL, "bot10", "main")

	select {
	case evt := <-received:
		if evt.Event != "login_success" {
			t.Errorf("event = %q, want %q", evt.Event, "login_success")
		}
		if evt.AccountID != "bot10" {
			t.Errorf("account_id = %q, want %q", evt.AccountID, "bot10")
		}
		if evt.LoginType != "main" {
			t.Errorf("login_type = %q, want %q", evt.LoginType, "main")
		}
		if evt.Timestamp == "" {
			t.Error("timestamp 不能为空")
		}
		if _, err := time.Parse(time.RFC3339, evt.Timestamp); err != nil {
			t.Errorf("timestamp 格式不是 RFC3339: %v", err)
		}
		t.Logf("收到回调: %+v", evt)
	case <-time.After(5 * time.Second):
		t.Fatal("超时: 未收到回调通知")
	}
}

// TestNotifyLoginSuccessTo_EmptyURL 验证 callbackURL 为空时不发送、不 panic
func TestNotifyLoginSuccessTo_EmptyURL(t *testing.T) {
	// 不应 panic，不应发送任何请求
	NotifyLoginSuccessTo("", "bot10", "main")
	t.Log("callbackURL 为空时正常返回，无 panic")
}
