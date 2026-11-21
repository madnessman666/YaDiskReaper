package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const AppName = "yareaper"

func ariaSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func LocalAriaPath() string {
	return filepath.Join("aria2c_bin", runtime.GOOS, "aria2c"+ariaSuffix())
}

func AriaCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir() // если программа запущена в голом докере/LXC без профиля пользователя
	}

	dir := filepath.Join(base, AppName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("couldn't create cache dir: %w", err)
	}

	return dir, nil
}

func EmbeddedAriaName(hash string) string {
	return fmt.Sprintf("%s_aria2c_%s%s", AppName, hash, ariaSuffix())
}
