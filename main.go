package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
)

var API_ENDPOINT = "https://cloud-api.yandex.net/v1/disk/public/resources/?public_key=%s&path=/%s&offset=%s"

type Directory struct {
	Path      string    `json:"path"`
	Type      string    `json:"type"` // "dir" or "file"
	Name      string    `json:"name"`
	PublicKey string    `json:"public_key"`
	File      string    `json:"file"`
	Embedded  *Embedded `json:"_embedded"`
}

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
	targetUrl string
	paralel   int
	outDir    string
)

func main() {
	flag.StringVar(&targetUrl, "u", "", "Url to yandex disk folder")
	flag.IntVar(&paralel, "p", 8, "Number of parallel downloading")
	flag.StringVar(&outDir, "o", "./", "Out dir for files")
	flag.Parse()

	if targetUrl == "" {
		fmt.Println("Error: Url is empty")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf(API_ENDPOINT+"\n", url.QueryEscape(targetUrl), "", "0")
}

func buildUrl() {

}

func RetriveYaDiskDir() {

}
