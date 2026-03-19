package configs

import "os"

var callbackURL string

// SetCallbackURL 设置回调地址
func SetCallbackURL(url string) {
	callbackURL = url
}

// GetCallbackURL 返回回调地址，优先使用显式设置值，其次读环境变量
func GetCallbackURL() string {
	if callbackURL != "" {
		return callbackURL
	}
	return os.Getenv("XHS_CALLBACK_URL")
}
