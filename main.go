package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

type DownloadTask struct {
	URL         string
	SavePath    string
	ExpectedMD5 string
	RemotePath  string
}


type Resource struct {
	Name      string    `json:"name"`
	Type      string    `json:"type"` // "dir" or "file"
	Path      string    `json:"path"`
	File      string    `json:"file"`
	Size      int64     `json:"size"`
	PublicKey string    `json:"public_key"`
	Embedded  *Embedded `json:"_embedded"`
	MD5       string    `json:"md5"`
}

type Embedded struct {
	Items  []Resource `json:"items"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
	Total  int        `json:"total"`
}

var (
	p         *mpb.Progress
	targetUrl string
	paralel   int
	outDir    string
	client    = &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ForceAttemptHTTP2:     true,
			ResponseHeaderTimeout: 15 * time.Second,
		},
	}
	failedTasks  []DownloadTask
	failedMu     sync.Mutex
	skippedCount int64
)

func main() {

	p = mpb.New(mpb.WithWidth(64))
	log.SetOutput(p)
	log.SetFlags(log.Ltime)

	log.Println("Yandex Downloader")
	flag.StringVar(&targetUrl, "u", "", "Url to yandex disk folder")
	flag.IntVar(&paralel, "p", 8, "Number of parallel downloading")
	flag.StringVar(&outDir, "o", "./out", "Out dir for files")
	flag.Parse()

	var downloadWg sync.WaitGroup

	if targetUrl == "" {
		log.Println("Error: Url is empty")
		flag.Usage()
		os.Exit(1)
	}

	// END OF CHECKS OF ARGS

	var (
		currentSpeedMB float64
		activeFiles    int
		waitingFiles   int
		telemetryMu    sync.RWMutex
	)

	ariaProcess, err := startAriaDaemon()

	if err != nil {
		log.Printf("❌ FUCK: %v\n", err)
		return
	}

	defer func() {
		if ariaProcess != nil && ariaProcess.Process != nil {
			ariaProcess.Process.Kill()
		}
	}()

	log.Println("🚀 Aria2c started! Check your belts!")

	aria, err := NewAriaClient("ws://localhost:6800/jsonrpc")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Getting tree of files...")

	allTasks, dirName, err := getAllTasks(targetUrl, "", outDir)
	if err != nil {
		log.Printf("Error white getting tree: %v\n", err)
		return
	}
	totalFiles := len(allTasks)

	log.Printf("📦 Found files: %d. Checking downloaded files!\n", totalFiles)

	totalBar := p.AddBar(int64(totalFiles),
		mpb.BarPriority(1000),
		mpb.PrependDecorators(
			decor.Name("📦 DOWNLOADING: ", decor.WC{W: 10}),
			decor.CountersNoUnit("%d / %d", decor.WC{W: 12}),
		),
		mpb.AppendDecorators(
			decor.Percentage(decor.WC{W: 5}),
			decor.Any(func(s decor.Statistics) string {
				return fmt.Sprintf(" | 🚀 %5.1f MB/с | 🔥 Active: %d | ⏳ Queue: %-4d",
					currentSpeedMB, activeFiles, waitingFiles)
			}),
		),
	)

	aria.OnStatsUpdate = func(speedMB float64, active, waiting int) {
		telemetryMu.Lock()
		currentSpeedMB = speedMB
		activeFiles = active
		waitingFiles = waiting
		telemetryMu.Unlock()
	}

	aria.OnDownloadComplete = func(task DownloadTask, gid string) {
		totalBar.Increment()
		downloadWg.Done()
	}

	aria.OnDownloadError = func(task DownloadTask, gid string) {
		downloadWg.Done()
		totalBar.Increment()

		failedMu.Lock()
		failedTasks = append(failedTasks, task)
		failedMu.Unlock()

		logFailedToFile(task)
	}

	rootDirName := dirName
	if rootDirName == "" {
		rootDirName = "yandex_download"
	}

	finalSaveDir := filepath.Join(outDir, rootDirName)

	log.Printf("📁 Downloading to folder: %s\n", finalSaveDir)

	for _, task := range allTasks {
		if verifyFileMD5(task.SavePath, task.ExpectedMD5) {
			atomic.AddInt64(&skippedCount, 1)
			totalBar.Increment()
		} else {
			aria.AddTask(task)
			downloadWg.Add(1)
		}
	}

	downloadWg.Wait()

	fmt.Println("\n==================================================")
	if len(failedTasks) == 0 {
		fmt.Println("🎉 SUCCESS! Everything downloaded!")
	} else {
		fmt.Printf("⚠️ WARNING: Errors while downloading: %d/%d\n", len(failedTasks), totalFiles)
		fmt.Println("📄 Log file: failed.log")
		fmt.Println("--------------------------------------------------")

		maxShow := 10
		if len(failedTasks) < maxShow {
			maxShow = len(failedTasks)
		}

		for i := 0; i < maxShow; i++ {
			fmt.Printf("❌ %s\n", filepath.Base(failedTasks[i].SavePath))
		}

		if len(failedTasks) > maxShow {
			fmt.Printf("... and %d files (check failed.log for more info)\n", len(failedTasks)-maxShow)
		}
	}
	fmt.Println("==================================================")
}