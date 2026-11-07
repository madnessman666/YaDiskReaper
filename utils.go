package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func getAriaExecutable() (string, error) {
	localAria := "./aria2c_bin/aria2c"
	if runtime.GOOS == "windows" {
		localAria += ".exe"
	}

	if _, err := os.Stat(localAria); err == nil {
		return localAria, nil
	}

	return extractEmbeddedAria()
}

func extractEmbeddedAria() (string, error) {
	binName := "yareaper_aria2c"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	tmpPath := filepath.Join(os.TempDir(), binName)

	if fi, err := os.Stat(tmpPath); err == nil {

		if fi.Size() == int64(len(embeddedAria)) {
			return tmpPath, nil
		}
	}

	err := os.WriteFile(tmpPath, embeddedAria, 0755)
	if err != nil {
		return "", fmt.Errorf("Couldn't unpack embedded aria2c: %w", err)
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
