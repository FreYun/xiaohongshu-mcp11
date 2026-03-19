package callback

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
)

// LoginSuccessEvent 登录成功事件
type LoginSuccessEvent struct {
	Event     string `json:"event"`
	AccountID string `json:"account_id"`
	LoginType string `json:"login_type"` // "main" / "creator" / "both"
	Timestamp string `json:"timestamp"`
}

// NotifyLoginSuccess 异步通知调用方登录成功，失败只打日志不阻塞
func NotifyLoginSuccess(accountID string, loginType string) {
	url := configs.GetCallbackURL()
	if url == "" {
		return
	}

	go func() {
		event := LoginSuccessEvent{
			Event:     "login_success",
			AccountID: accountID,
			LoginType: loginType,
			Timestamp: time.Now().Format(time.RFC3339),
		}

		body, err := json.Marshal(event)
		if err != nil {
			logrus.Warnf("callback: 序列化失败: %v", err)
			return
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			logrus.Warnf("callback: POST %s 失败: %v", url, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 300 {
			logrus.Warnf("callback: POST %s 返回 %d", url, resp.StatusCode)
			return
		}

		logrus.Infof("callback: 登录成功通知已发送 account=%s type=%s", accountID, loginType)
	}()
}
