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
)

type DownloadTask struct {
	URL         string
	SavePath    string
	ExpectedMD5 string
	RemotePath  string
	Size        int64
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
	totalBar  *mpb.Bar
	targetUrl string
	parallel  int
	outDir    string
	scanOnly  bool
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
	failedTasks []DownloadTask
	failedMu    sync.Mutex

	// Counters shared with the JSON event stream (and therefore the GUI).
	skippedCount   int64
	doneFiles      int64
	failedCount    int64
	totalFileCount int64
	totalBytes     int64
	bytesFinished  int64
	scanFiles      int64
	scanBytesFound int64

	activityMu      sync.RWMutex
	currentActivity string
)

func setActivity(msg string) {
	activityMu.Lock()
	currentActivity = msg
	activityMu.Unlock()
}

func getActivity() string {
	activityMu.RLock()
	defer activityMu.RUnlock()
	return currentActivity
}

// finishTask marks a file as processed (downloaded or skipped by MD5).
func finishTask(task DownloadTask, skipped bool) {
	atomic.AddInt64(&doneFiles, 1)
	atomic.AddInt64(&bytesFinished, task.Size)
	if skipped {
		atomic.AddInt64(&skippedCount, 1)
	}
	if totalBar != nil {
		totalBar.Increment()
	}
	emit("file_done", map[string]any{
		"path":    task.SavePath,
		"size":    task.Size,
		"skipped": skipped,
	})
}

// recordFailure marks a file as permanently failed.
func recordFailure(task DownloadTask, reason string) {
	atomic.AddInt64(&doneFiles, 1)
	atomic.AddInt64(&bytesFinished, task.Size)
	atomic.AddInt64(&failedCount, 1)
	if totalBar != nil {
		totalBar.Increment()
	}

	failedMu.Lock()
	failedTasks = append(failedTasks, task)
	failedMu.Unlock()

	emit("file_failed", map[string]any{
		"path":  task.SavePath,
		"error": reason,
	})
	logFailedToFile(task)
}

func main() {
	flag.StringVar(&targetUrl, "u", "", "Url to yandex disk folder")
	flag.IntVar(&parallel, "p", 8, "Number of parallel downloading")
	flag.StringVar(&outDir, "o", "./out", "Out dir for files")
	flag.BoolVar(&jsonMode, "json", false, "Emit machine-readable JSON events to stdout (logs go to stderr)")
	flag.BoolVar(&scanOnly, "scan-only", false, "Only scan the folder and report totals")
	flag.Parse()

	if jsonMode {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.Ltime)
	} else {
		p = mpb.New(mpb.WithWidth(64))
		log.SetOutput(p)
		log.SetFlags(log.Ltime)
		log.Println("Yandex Downloader")
	}

	emit("starting", nil)

	var downloadWg sync.WaitGroup

	if targetUrl == "" {
		emit("fatal", map[string]any{"message": "url is empty"})
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
		fatalEvent(fmt.Sprintf("couldn't start aria2c: %v", err))
	}

	defer func() {
		if ariaProcess != nil && ariaProcess.Process != nil {
			ariaProcess.Process.Kill()
		}
	}()

	log.Println("🚀 Aria2c started! Check your belts!")

	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/jsonrpc", ariaPort)
	aria, err := connectAria(wsURL)
	if err != nil {
		fatalEvent(fmt.Sprintf("couldn't connect to aria2c: %v", err))
	}

	info, err := getInfo(targetUrl)

	if err != nil {
		fatalEvent(fmt.Sprintf("couldn't get info due to error: %v", err))
	}

	emit("info", map[string]any{"name": info.Name, "type": info.Type})

	if info.Type == "dir" {
		outDir = filepath.Join(outDir, info.Name)
	}

	log.Println("Getting tree of files...")
	emit("scanning", map[string]any{"path": targetUrl})

	stopScanReporter := startScanReporter()
	allTasks, _, err := getAllTasks(targetUrl, "", outDir)
	stopScanReporter()
	if err != nil {
		fatalEvent(fmt.Sprintf("error while getting tree: %v", err))
	}

	totalFiles := len(allTasks)
	var scanBytes int64
	for _, task := range allTasks {
		scanBytes += task.Size
	}
	atomic.StoreInt64(&totalFileCount, int64(totalFiles))
	atomic.StoreInt64(&totalBytes, scanBytes)
	emit("scan_done", map[string]any{"total": totalFiles, "bytes": scanBytes})

	if scanOnly {
		emit("finished", map[string]any{
			"ok":        true,
			"total":     totalFiles,
			"skipped":   0,
			"failed":    0,
			"bytes":     scanBytes,
			"scan_only": true,
		})
		if !jsonMode {
			fmt.Printf("\n📦 Found %d files, %.1f MiB total\n", totalFiles, float64(scanBytes)/1024/1024)
		}
		return
	}

	if !jsonMode {
		totalBar = p.AddBar(int64(totalFiles),
			mpb.BarPriority(1000),
			mpb.PrependDecorators(
				decor.Name("📦 DOWNLOADING: ", decor.WC{W: 10}),
				decor.CountersNoUnit("%d / %d", decor.WC{W: 12}),
				decor.Any(func(s decor.Statistics) string {
					return fmt.Sprintf(" ⚙️  %-25.25s", getActivity())
				}, decor.WC{W: 25}),
			),
			mpb.AppendDecorators(
				decor.Percentage(decor.WC{W: 5}),
				decor.Any(func(s decor.Statistics) string {
					telemetryMu.RLock()
					defer telemetryMu.RUnlock()
					return fmt.Sprintf(" | 🚀 %5.1f MB/с | 🔥 Active: %d | ⏳ Queue: %-4d",
						currentSpeedMB, activeFiles, waitingFiles)
				}),
			),
		)
	}

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

	aria.OnStatsUpdate = func(stats AriaStats) {
		telemetryMu.Lock()
		currentSpeedMB = stats.SpeedBytes / 1024 / 1024
		activeFiles = stats.Active
		waitingFiles = stats.Waiting
		telemetryMu.Unlock()

		emitProgress(stats)
	}

	aria.OnDownloadComplete = func(task DownloadTask, gid string) {
		finishTask(task, false)
		downloadWg.Done()
	}

	aria.OnDownloadStart = func(task DownloadTask, gid string) {
		setActivity("⬇️  " + filepath.Base(task.SavePath))
	}

	aria.OnDownloadError = func(task DownloadTask, gid string) {
		if task.Retries >= 3 {
			log.Printf("[%s] Limit of tries ended.", filepath.Base(task.SavePath))
			recordFailure(task, "retry limit reached")
			downloadWg.Done()
			return
		}
		task.Retries++
		log.Printf("[%s] Got error while trying download. Retrying... %d/3", task.SavePath, task.Retries)
		freshURL, err := getFreshURL(targetUrl, task.RemotePath)
		if err != nil {
			log.Printf("❌ Can't get new url: %v", err)
			recordFailure(task, fmt.Sprintf("couldn't refresh download url: %v", err))
			downloadWg.Done()
			return
		}
		task.URL = freshURL
		err = aria.AddTask(task)
		if err != nil {
			log.Printf("Failed to add file %s. Error: %s", task.RemotePath, err)
			recordFailure(task, err.Error())
			downloadWg.Done()
		}
	}

	for _, task := range allTasks {
		setActivity("🔍 MD5: " + filepath.Base(task.SavePath))
		if verifyFileMD5(task.SavePath, task.ExpectedMD5) {
			finishTask(task, true)
		} else {
			downloadWg.Add(1)
			err := aria.AddTask(task)
			if err != nil {
				log.Printf("Failed to add file %s. Error: %s", task.RemotePath, err)
				recordFailure(task, err.Error())
				downloadWg.Done()
			}
		}
	}
	setActivity("Finishing")
	downloadWg.Wait()
	setActivity("Finished")

	emit("finished", map[string]any{
		"ok":      atomic.LoadInt64(&failedCount) == 0,
		"total":   totalFiles,
		"skipped": atomic.LoadInt64(&skippedCount),
		"failed":  atomic.LoadInt64(&failedCount),
		"bytes":   atomic.LoadInt64(&bytesFinished),
	})

	if jsonMode {
		if atomic.LoadInt64(&failedCount) > 0 {
			os.Exit(1)
		}
		os.Exit(0)
	}

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
