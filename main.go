package main

import (
	"flag"
	"fmt"
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
	"golang.org/x/time/rate"
)

type DownloadTask struct {
	URL         string
	SavePath    string
	ExpectedMD5 string
	RemotePath  string
	Retries     int
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
	parallel  int
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
	failedTasks     []DownloadTask
	failedMu        sync.Mutex
	skippedCount    int64
	currentActivity string
	apiLimiter      = rate.NewLimiter(rate.Every(5*time.Second), 1)
)

func main() {

	p = mpb.New(mpb.WithWidth(64))
	log.SetOutput(p)
	log.SetFlags(log.Ltime)

	log.Println("Yandex Downloader")
	flag.StringVar(&targetUrl, "u", "", "Url to yandex disk folder")
	flag.IntVar(&parallel, "p", 8, "Number of parallel downloading")
	flag.StringVar(&outDir, "o", "./out", "Out dir for files")
	flag.Parse()

	var downloadWg sync.WaitGroup

	if targetUrl == "" {
		log.Println("Error: Url is empty")
		flag.Usage()
		os.Exit(1)
	}

	if parallel < 1 {
		parallel = 1
	}

	if parallel > 16 {
		log.Println("⚠️  WARNING: Max value for -p is 16. Setting to 16.")
		parallel = 16
	}
	// END OF CHECKS OF ARGS

	var (
		currentSpeedMB float64
		activeFiles    int
		waitingFiles   int
		telemetryMu    sync.RWMutex
	)
	ariaPort, err := GetFreePort()
	if err != nil {
		log.Println("Can't allocate random port for aria. Setting 6800!")
		ariaPort = 6800
	}
	ariaProcess, err := startAriaDaemon(ariaPort)

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

	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/jsonrpc", ariaPort)
	aria, err := NewAriaClient(wsURL)
	if err != nil {
		log.Fatal(err)
	}

	info, err := getInfo(targetUrl)

	if err != nil {
		log.Fatal("Couldn't get info due to error: ", err)
	}

	if info.Type == "dir" {
		outDir = filepath.Join(outDir, info.Name)
	}

	log.Println("Getting tree of files...")

	allTasks, _, err := getAllTasks(targetUrl, "", outDir)
	if err != nil {
		log.Printf("Error white getting tree: %v\n", err)
		return
	}

	totalFiles := len(allTasks)

	totalBar := p.AddBar(int64(totalFiles),
		mpb.BarPriority(1000),
		mpb.PrependDecorators(
			decor.Name("📦 DOWNLOADING: ", decor.WC{W: 10}),
			decor.CountersNoUnit("%d / %d", decor.WC{W: 12}),
			decor.Any(func(s decor.Statistics) string {
				return fmt.Sprintf(" ⚙️  %-25.25s", currentActivity)
			}, decor.WC{W: 25}),
		),
		mpb.AppendDecorators(
			decor.Percentage(decor.WC{W: 5}),
			decor.Any(func(s decor.Statistics) string {
				return fmt.Sprintf(" | 🚀 %5.1f MB/с | 🔥 Active: %d | ⏳ Queue: %-4d",
					currentSpeedMB, activeFiles, waitingFiles)
			}),
		),
	)

	log.Printf("📦 Found files: %d!\n", totalFiles)

	rootDirName := outDir
	if rootDirName == "" {
		rootDirName = "yandex_download"
	}

	finalSaveDir, err := filepath.Abs(outDir)

	if err != nil {
		log.Printf("Error while resolving path: %s", err)
	}

	log.Printf("📁 Downloading to: %s\n", finalSaveDir)

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
		if task.Retries >= 3 {
			log.Printf("[%s] Limit of tries ended.", filepath.Base(task.SavePath))
			totalBar.Increment()
			downloadWg.Done()

			failedMu.Lock()
			failedTasks = append(failedTasks, task)
			failedMu.Unlock()

			logFailedToFile(task)
			return
		}
		task.Retries++
		log.Printf("[%s] Got error while trying download. Retrying... %d/3", task.SavePath, task.Retries)
		freshURL, err := getFreshURL(targetUrl, task.RemotePath)
		if err != nil {
			log.Printf("❌ Can't get new url: %v", err)

			totalBar.Increment()
			downloadWg.Done()

			failedMu.Lock()
			failedTasks = append(failedTasks, task)
			failedMu.Unlock()
			logFailedToFile(task)
			return
		}
		task.URL = freshURL
		err = aria.AddTask(task)
		if err != nil {
			log.Printf("Failed to add file %s. Error: %s", task.RemotePath, err)
			downloadWg.Done()
		}
	}

	for _, task := range allTasks {
		currentActivity = "🔍 MD5: " + filepath.Base(task.SavePath)
		if verifyFileMD5(task.SavePath, task.ExpectedMD5) {
			atomic.AddInt64(&skippedCount, 1)
			totalBar.Increment()
		} else {
			downloadWg.Add(1)
			err := aria.AddTask(task)
			if err != nil {
				log.Printf("Failed to add file %s. Error: %s", task.RemotePath, err)
				downloadWg.Done()
			}
		}
	}
	currentActivity = "DL remaining files"
	downloadWg.Wait()

	fmt.Println("\n==================================================")
	if len(failedTasks) == 0 {
		fmt.Printf("🎉 SUCCESS! Everything downloaded! Skipped: %d\n", skippedCount)
		fmt.Println("==================================================")
		os.Exit(0)
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
		fmt.Println("==================================================")
		os.Exit(1)
	}
}
