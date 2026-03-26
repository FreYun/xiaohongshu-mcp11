package cookies

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

type Cookier interface {
	LoadCookies() ([]byte, error)
	SaveCookies(data []byte) error
	DeleteCookies() error
}

type localCookie struct {
	path string
}

func NewLoadCookie(path string) Cookier {
	if path == "" {
		panic("path is required")
	}

	return &localCookie{
		path: path,
	}
}

// LoadCookies 从文件中加载 cookies。
func (c *localCookie) LoadCookies() ([]byte, error) {

	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read cookies from tmp file")
	}

	return data, nil
}

// SaveCookies 保存 cookies 到文件中。
func (c *localCookie) SaveCookies(data []byte) error {
	return os.WriteFile(c.path, data, 0644)
}

// DeleteCookies 删除 cookies 文件。
func (c *localCookie) DeleteCookies() error {
	if _, err := os.Stat(c.path); os.IsNotExist(err) {
		// 文件不存在，返回 nil（认为已经删除）
		return nil
	}
	return os.Remove(c.path)
}

// 全局 cookie 路径，通过 SetCookiesFilePath 设置
var customCookiesPath string

// SetCookiesFilePath 设置当前实例的 cookie 文件路径（每个 bot 应使用不同路径）
func SetCookiesFilePath(path string) {
	customCookiesPath = path
}

// GetCookiesFilePath 获取 cookies 文件路径。
// 优先级：SetCookiesFilePath > COOKIES_PATH 环境变量 > /tmp/cookies.json > cookies.json
func GetCookiesFilePath() string {
	if customCookiesPath != "" {
		return customCookiesPath
	}

	path := os.Getenv("COOKIES_PATH")
	if path != "" {
		return path
	}

	// 旧路径兼容
	tmpDir := os.TempDir()
	oldPath := filepath.Join(tmpDir, "cookies.json")
	if _, err := os.Stat(oldPath); err == nil {
		return oldPath
	}

	return "cookies.json"
}

// GetCookiesFilePathForBot 返回指定 bot 的 cookie 文件路径。
// botID 为空时 fallback 到全局 GetCookiesFilePath()。
func GetCookiesFilePathForBot(botID string) string {
	if botID == "" {
		return GetCookiesFilePath()
	}
	return fmt.Sprintf("cookies-%s.json", botID)
}
