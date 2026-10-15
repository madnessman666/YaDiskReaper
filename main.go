package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// var API_ENDPOINT = "https://cloud-api.yandex.net/v1/disk/public/resources/?public_key=%s&path=/%s&offset=%s"

type DownloadTask struct {
	URL      string
	SavePath string
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
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}
	tasksChan chan DownloadTask
)

func main() {

	p = mpb.New(mpb.WithWidth(64))
	tasksChan = make(chan DownloadTask, 1000)

	fmt.Println("Yandex Downloader")
	flag.StringVar(&targetUrl, "u", "", "Url to yandex disk folder")
	flag.IntVar(&paralel, "p", 8, "Number of parallel downloading")
	flag.StringVar(&outDir, "o", "./out", "Out dir for files")
	flag.Parse()

	var wg sync.WaitGroup

	for i := 0; i < paralel; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for task := range tasksChan {
				err := downloadFile(task)
				if err != nil {
				}
			}
		}(i)
	}

	if targetUrl == "" {
		fmt.Println("Error: Url is empty")
		flag.Usage()
		os.Exit(1)
	}

	resp, err := client.Get(buildUrlForFolder(targetUrl, "", 0, 20))
	if err != nil {
		fmt.Println("Error while making req for retrieving: " + err.Error())
		os.Exit(1)
	}

	var res Resource
	err = json.NewDecoder(resp.Body).Decode(&res)

	resp.Body.Close()

	if err != nil {
		fmt.Printf("Error while parsing JSON: %v\n", err)
		return
	}
	for _, item := range res.Embedded.Items {
		if item.Type == "file" {
			task := DownloadTask{
				URL:      item.File,
				SavePath: filepath.Join(outDir, item.Name),
			}
			tasksChan <- task
		}
	}
	close(tasksChan)
	wg.Wait()
	p.Wait()
}

func buildUrlForFolder(publicKey string, remotePath string, offset int, limit int) string {
	baseURL := "https://cloud-api.yandex.net/v1/disk/public/resources"
	params := url.Values{}
	params.Add("public_key", publicKey)
	if remotePath != "" {
		params.Add("path", remotePath)
	} else {
		params.Add("path", "/")
	}
	params.Add("offset", strconv.Itoa(offset))
	params.Add("limit", strconv.Itoa(limit))
	return baseURL + "/?" + params.Encode()
}

func RetriveYaDiskDir(publicKey string, path string, localDir string) {

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

	bar := p.AddBar(0,
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
	if err != nil {
		statusMsg = "❌ Error while connecting"
		bar.Abort(false)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		statusMsg = "❌ Server returned " + resp.Status
		bar.Abort(false)
		return fmt.Errorf("status: %s", resp.Status)
	}

	isConnected = true
	bar.SetTotal(resp.ContentLength, false)

	out, err := os.Create(task.SavePath)
	if err != nil {
		bar.Abort(false)
		return err
	}
	defer out.Close()
	proxyReader, err := bar.ProxyReader(resp.Body)
	defer proxyReader.Close()
	_, err = io.Copy(out, proxyReader)
	if err != nil {
		bar.Abort(false)
		return err
	}
	bar.SetTotal(bar.Current(), true)
	return nil
}
