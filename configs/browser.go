package configs

var (
	useHeadless = true

	binPath    = ""
	profileDir = ""
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
