package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func getAriaExecutable() (string, error) {
	localAria := "./aria2c_bin/aria2c"
	if runtime.GOOS == "windows" {
		localAria += ".exe"
	} else if runtime.GOOS == "darwin" {
		localAria += "_darwin"
	}

	if _, err := os.Stat(localAria); err == nil {
		return localAria, nil
	}

	return extractEmbeddedAria()
}

func extractEmbeddedAria() (string, error) {
	h := sha256.Sum256(embeddedAria)
	hash := fmt.Sprintf("%x", h[:4])

	binName := fmt.Sprintf("yareaper_aria2c_%s", hash)

	if runtime.GOOS == "windows" {
		binName += ".exe"
	} else if runtime.GOOS == "darwin" {
		binName += "_darwin"
	}

	tmpDir, err := os.UserCacheDir()
	if err != nil {
		tmpDir = os.TempDir() // если программа запущена в голом докере/LXC без профиля пользователя
	}
	appDir := filepath.Join(tmpDir, "yareaper")
	_ = os.MkdirAll(appDir, 0755)

	tmpPath := filepath.Join(appDir, binName)

	if fi, err := os.Stat(tmpPath); err == nil {

		if fi.Size() == int64(len(embeddedAria)) {
			return tmpPath, nil
		}
	}

	tmpFile, err := os.CreateTemp(appDir, "aria2c_*.tmp")

	if err != nil {
		return "", fmt.Errorf("couldn't create temp file: %w", err)
	}

	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(embeddedAria); err != nil {
		tmpFile.Close()
		return "", err
	}
	if err := tmpFile.Chmod(0755); err != nil {
		tmpFile.Close()
		return "", err
	}
	tmpFile.Close()

	if err := os.Rename(tmpFile.Name(), tmpPath); err != nil {
		if runtime.GOOS == "windows" {
			if _, statErr := os.Stat(tmpPath); statErr == nil {
				return tmpPath, nil
			}
		}
		return "", fmt.Errorf("couldn't move binary to target: %w", err)
	}

	return tmpPath, nil
}

func logFailedToFile(task DownloadTask) {
	f, err := os.OpenFile("failed.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		defer f.Close()
		f.WriteString(fmt.Sprintf("%s\n", task.SavePath))
	}
}
