package main

import (
	"encoding/json"
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

// var API_ENDPOINT = "https://cloud-api.yandex.net/v1/disk/public/resources/?public_key=%s&path=/%s&offset=%s"

type DownloadTask struct {
	URL         string
	SavePath    string
	ExpectedMD5 string
	RemotePath  string
}

// type Directory struct {
// 	Path      string    `json:"path"`
// 	Type      string    `json:"type"` // "dir" or "file"
// 	Name      string    `json:"name"`
// 	PublicKey string    `json:"public_key"`
// 	File      string    `json:"file"`
// 	Embedded  *Embedded `json:"_embedded"`
// }

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
				Timeout:   10 * time.Second, // Если за 10 сек не подключился к IP - отбой
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
	tasksChan    chan DownloadTask
	failedTasks  []DownloadTask
	failedMu     sync.Mutex
	skippedCount int64
)

func main() {

	p = mpb.New(mpb.WithWidth(64))

	fmt.Println("Yandex Downloader")
	flag.StringVar(&targetUrl, "u", "", "Url to yandex disk folder")
	flag.IntVar(&paralel, "p", 8, "Number of parallel downloading")
	flag.StringVar(&outDir, "o", "./out", "Out dir for files")
	flag.Parse()

	var downloadWg sync.WaitGroup

	if targetUrl == "" {
		fmt.Println("Error: Url is empty")
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
		fmt.Printf("❌ Пиздец: %v\n", err)
		return
	}

	defer func() {
		if ariaProcess != nil && ariaProcess.Process != nil {
			ariaProcess.Process.Kill()
		}
	}()

	fmt.Println("🚀 Aria2c started! Check your belts!")

	aria, err := NewAriaClient("ws://localhost:6800/jsonrpc")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Getting tree of files...")

	allTasks, dirName, err := getAllTasks(targetUrl, "", outDir)
	if err != nil {
		fmt.Printf("Ошибка обхода: %v\n", err)
		return
	}
	totalFiles := len(allTasks)
	fmt.Printf("📦 Found files: %d. Checking downloaded files!\n", totalFiles)

	// tasksChan = make(chan DownloadTask, totalFiles)

	totalBar := p.AddBar(int64(totalFiles),
		mpb.BarPriority(1000),
		mpb.PrependDecorators(
			decor.Name("📦 ВЫКАЧКА: ", decor.WC{W: 10}),
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

		failedMu.Lock()
		failedTasks = append(failedTasks, task)
		failedMu.Unlock()

		logFailedToFile(task)
	}

	// for i := 0; i < paralel; i++ {
	// 	wg.Add(1)
	// 	go func(workerID int) {
	// 		defer wg.Done()
	// 		for task := range tasksChan {
	// 			err := downloadFile(task)
	// 			if err != nil {
	// 				failedMu.Lock()
	// 				failedTasks = append(failedTasks, task)
	// 				failedMu.Unlock()
	// 			}
	// 			totalBar.Increment()
	// 		}
	// 	}(i)
	// }
	// for _, task := range allTasks {
	// 	tasksChan <- task
	// }

	rootDirName := dirName
	if rootDirName == "" {
		rootDirName = "yandex_download"
	}

	finalSaveDir := filepath.Join(outDir, rootDirName)

	fmt.Printf("📁 Downloading to folder: %s\n", finalSaveDir)

	for _, task := range allTasks {
		if verifyFileMD5(task.SavePath, task.ExpectedMD5) {
			atomic.AddInt64(&skippedCount, 1)
		} else {
			aria.AddTask(task)
			downloadWg.Add(1)
		}
	}

	// RetriveYaDiskDir(res.PublicKey, res.Path, outDir)
	// for _, item := range res.Embedded.Items {
	// 	if item.Type == "file" {
	// 		task := DownloadTask{
	// 			URL:      item.File,
	// 			SavePath: filepath.Join(outDir, item.Name),
	// 		}
	// 		tasksChan <- task
	// 	}
	// }

	downloadWg.Wait()

	// if len(failedTasks) > 0 {
	// 	fmt.Printf("\n⚠️ Первый заход окончен. Упало файлов: %d. Начинаем операцию добивания (Раунд 2)...\n", len(failedTasks))
	// 	time.Sleep(5 * time.Second) // Даем серверам 5 сек оклематься

	// 	retryChan := make(chan DownloadTask, len(failedTasks))
	// 	var retryWg sync.WaitGroup

	// 	retryParallel := 3
	// 	for i := 0; i < retryParallel; i++ {
	// 		retryWg.Add(1)
	// 		go func() {
	// 			defer retryWg.Done()
	// 			for task := range retryChan {
	// 				// Пробуем скачать повторно
	// 				downloadFile(task)
	// 			}
	// 		}()
	// 	}

	// 	for _, task := range failedTasks {
	// 		retryChan <- task
	// 	}
	// 	close(retryChan)

	// 	// Ждем завершения второго захода
	// 	retryWg.Wait()
	// }

	// p.Wait()

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

func RetriveYaDiskDir(publicKey, remotePath, localDir string) {
	offset := 0
	limit := 100

	for {
		reqURL := buildUrlForFolder(publicKey, remotePath, offset, limit)
		resp, err := client.Get(reqURL)
		if err != nil {
			return
		}

		var res Resource
		json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()

		if res.Embedded == nil {
			break
		}

		for _, item := range res.Embedded.Items {
			if item.Type == "dir" {
				RetriveYaDiskDir(publicKey, item.Path, filepath.Join(localDir, item.Name))
			} else {
				tasksChan <- DownloadTask{
					URL:      item.File,
					SavePath: filepath.Join(localDir, item.Name),
				}
			}
		}

		offset += limit
		if offset >= res.Embedded.Total {
			break
		}
	}
}

func downloadFile(task DownloadTask) error {
	fileName := filepath.Base(task.SavePath)
	if len(fileName) > 20 {
		fileName = fileName[:17] + "..."
	}

	runningName := fmt.Sprintf("%-25s", "⏳ "+fileName)
	doneName := fmt.Sprintf("%-25s", "✅ "+fileName)
	statusMsg := "🌐 Connecting to server..."
	isConnected := false
	if verifyFileMD5(task.SavePath, task.ExpectedMD5) {
		// doneName := fmt.Sprintf("%-25s", "⚡ "+fileName)

		// skipBar := p.AddBar(1,
		// 	mpb.BarPriority(999-paralel),
		// 	mpb.PrependDecorators(
		// 		decor.Name(doneName),
		// 		decor.Name(" [HASH MATCHED] ", decor.WC{W: 24}),
		// 	),
		// 	mpb.AppendDecorators(
		// 		decor.Name("OK", decor.WC{W: 5}),
		// 	),
		// )
		// skipBar.SetTotal(1, true)
		atomic.AddInt64(&skippedCount, 1)
		return nil
	}
	bar := p.AddBar(0,
		mpb.BarPriority(999),
		mpb.BarRemoveOnComplete(),
		mpb.PrependDecorators(
			decor.OnComplete(
				decor.Name(runningName),
				doneName,
			),
			decor.Any(func(s decor.Statistics) string {
				if !isConnected {
					return statusMsg
				}
				current := float64(s.Current) / 1024 / 1024
				total := float64(s.Total) / 1024 / 1024
				return fmt.Sprintf("%6.1f / %-6.1f MiB", current, total)
			}, decor.WC{W: 24}),
		),
		mpb.AppendDecorators(
			decor.Any(func(s decor.Statistics) string {
				if !isConnected || s.Total <= 0 {
					return ""
				}
				pct := float64(s.Current) / float64(s.Total) * 100
				return fmt.Sprintf("%5.1f%%", pct)
			}, decor.WC{W: 6}),
			decor.EwmaSpeed(decor.SizeB1024(0), " % .1f", 60),
		),
	)

	err := os.MkdirAll(filepath.Dir(task.SavePath), 0755)
	if err != nil {
		return fmt.Errorf("Can't create folder: %w", err)
	}

	resp, err := client.Get(task.URL)
	if err != nil || resp.StatusCode >= 500 {
		if resp != nil {
			resp.Body.Close()
		}

		statusMsg = "🔄 500! Grabbing new url..."
		time.Sleep(1 * time.Second)

		freshURL, freshErr := getFreshURL(targetUrl, task.RemotePath)
		if freshErr == nil && freshURL != "" {
			resp, err = client.Get(freshURL)
		}
	}
	if err != nil {
		statusMsg = "❌ Error while connecting"
		bar.Abort(false)
		p.UpdateBarPriority(bar, 0, false)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		statusMsg = "❌ Server returned " + resp.Status
		bar.Abort(false)
		p.UpdateBarPriority(bar, 0, false)
		return fmt.Errorf("status: %s", resp.Status)
	}

	isConnected = true
	bar.SetTotal(resp.ContentLength, false)

	out, err := os.Create(task.SavePath)
	if err != nil {
		bar.Abort(false)
		p.UpdateBarPriority(bar, 0, false)
		return err
	}
	defer out.Close()
	proxyReader, err := bar.ProxyReader(resp.Body)
	defer proxyReader.Close()
	_, err = io.Copy(out, proxyReader)
	if err != nil {
		bar.Abort(false)
		p.UpdateBarPriority(bar, 0, false)
		return err
	}
	bar.SetTotal(bar.Current(), true)
	p.UpdateBarPriority(bar, 0, false)
	return nil
}
