package configs

import (
	"os"
	"path/filepath"
)

var (
	useHeadless = true

	binPath    = ""
	profileDir = ""

	// profilesBase 是多租户模式下的 profiles 根目录
	profilesBase = "/home/rooot/.xhs-profiles"
)

func InitHeadless(h bool) {
	useHeadless = h
}

// IsHeadless 是否无头模式。
func IsHeadless() bool {
	return useHeadless
}

func SetBinPath(b string) {
	binPath = b
}

func GetBinPath() string {
	return binPath
}

func SetProfileDir(d string) {
	profileDir = d
}

func GetProfileDir() string {
	return profileDir
}

func SetProfilesBase(base string) {
	profilesBase = base
}

func GetProfilesBase() string {
	return profilesBase
}

// GetProfileDirForBot 返回指定 bot 的 Chrome profile 目录。
// 优先 /home/rooot/.xhs-profiles/{botID}/，其次 /home/rooot/.openclaw/browser/{botID}/user-data。
// 都不存在返回空（走 cookie-file 模式）。
func GetProfileDirForBot(botID string) string {
	if botID == "" {
		return profileDir // fallback 到全局
	}

	// 优先检查 xhs-profiles
	p1 := filepath.Join(profilesBase, botID)
	if info, err := os.Stat(p1); err == nil && info.IsDir() {
		return p1
	}

	// 其次检查 openclaw browser 目录
	p2 := filepath.Join("/home/rooot/.openclaw/browser", botID, "user-data")
	if info, err := os.Stat(p2); err == nil && info.IsDir() {
		return p2
	}

	return ""
}
