package main

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"

	"yadiskreaper/internal/paths"
)

func getAriaExecutable() (string, error) {
	localAria := paths.LocalAriaPath()
	if _, err := os.Stat(localAria); err == nil {
		return localAria, nil
	}

	return extractEmbeddedAria()
}

func GetFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port, nil
}

func extractEmbeddedAria() (string, error) {
	h := sha256.Sum256(embeddedAria)
	hash := fmt.Sprintf("%x", h[:4])

	binName := paths.EmbeddedAriaName(hash)

	appDir, err := paths.AriaCacheDir()
	if err != nil {
		return "", err
	}

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
