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
)

func main() {
	flag.StringVar(&targetUrl, "u", "", "Url to yandex disk folder")
	flag.IntVar(&paralel, "p", 8, "Number of parallel downloading")
	flag.StringVar(&outDir, "o", "./out", "Out dir for files")
	flag.Parse()

	p = mpb.New(mpb.WithWidth(64))

	if targetUrl == "" {
		fmt.Println("Error: Url is empty")
		flag.Usage()
		os.Exit(1)
	}
	resp, err := client.Get(buildUrlForFolder(targetUrl, "", 0, 20))
	if err != nil {
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
			downloadFile(task)
		}
	}
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

func RetriveYaDiskDir() {

}

func downloadFile(task DownloadTask) error {
	fmt.Println(task)
	err := os.MkdirAll(filepath.Dir(task.SavePath), 0755)
	if err != nil {
		return fmt.Errorf("Can't create folder: %w", err)
	}
	resp, err := client.Get(task.URL)
	if err != nil {
		return fmt.Errorf("Network bullshit: %w", err)
	}
	defer resp.Body.Close()
	out, err := os.Create(task.SavePath)
	if err != nil {
		return fmt.Errorf("Can't create file: %w", err)
	}
	defer out.Close()
	bar := p.AddBar(resp.ContentLength,
		mpb.PrependDecorators(
			decor.Name(task.SavePath, decor.WC{W: 20}),
			decor.CountersKibiByte("% .2f / % .2f"), // сколько скачано в МБ
		),
		mpb.AppendDecorators(
			decor.EwmaSpeed(decor.SizeB1024(0), "% .2f", 60), // Скорость!
			decor.Percentage(decor.WC{W: 5}),                 // Проценты
		),
	)
	proxyReader, err := bar.ProxyReader(resp.Body)
	defer proxyReader.Close()
	_, err = io.Copy(out, proxyReader)
	if err != nil {
		return fmt.Errorf("Write failed: %w", err)
	}
	return nil
}
