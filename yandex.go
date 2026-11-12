package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

var visitedPaths = make(map[string]bool)

const bufferSize = 512 * 1024

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, bufferSize)
		return &b
	},
}

var hashPool = sync.Pool{
	New: func() any {
		return md5.New()
	},
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

func getInfo(publicKey string) (Resource, error) {
	name := ""
	reqURL := buildUrlForFolder(publicKey, "/", 0, 1)
	resp, err := client.Get(reqURL)

	if err != nil {
		return Resource{}, err
	}

	var res Resource

	json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()

	if name == "" {
		name = res.Name
	}

	return res, nil
}

func getAllTasks(publicKey, remotePath, localDir string) ([]DownloadTask, string, error) {
	var tasks []DownloadTask
	offset := 0
	limit := 100
	name := ""
	for {
		reqURL := buildUrlForFolder(publicKey, remotePath, offset, limit)
		resp, err := client.Get(reqURL)
		if err != nil {
			return nil, "", err
		}

		var res Resource
		json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		if name == "" {
			name = res.Name
		}
		if res.Type == "file" {
			tasks = append(tasks, DownloadTask{URL: res.File, SavePath: filepath.Join(localDir, res.Name), ExpectedMD5: res.MD5, RemotePath: res.Path, Retries: 0})
			return tasks, res.Name, nil
		}

		if res.Embedded != nil {
			for _, item := range res.Embedded.Items {
				currLocal := filepath.Join(localDir, item.Name)
				if visitedPaths[item.Path] {
					continue
				}
				visitedPaths[item.Path] = true
				if item.Type == "dir" {
					sub, _, _ := getAllTasks(publicKey, item.Path, currLocal)
					tasks = append(tasks, sub...)
				} else {
					tasks = append(tasks, DownloadTask{URL: item.File, SavePath: currLocal, ExpectedMD5: item.MD5, RemotePath: item.Path, Retries: 0})
				}
			}
			if len(res.Embedded.Items) == 0 {
				break
			}

			if len(res.Embedded.Items) < limit {
				break
			}

			offset += limit

			if offset >= res.Embedded.Total {
				break
			}

		} else {
			break
		}
	}
	return tasks, name, nil
}

func verifyFileMD5(filePath, expectedMD5 string) bool {
	if len(expectedMD5) != md5.Size*2 {
		return false
	}

	var expected [md5.Size]byte
	if _, err := hex.Decode(expected[:], []byte(expectedMD5)); err != nil {
		return false
	}

	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	h := hashPool.Get().(hash.Hash)
	h.Reset()
	defer hashPool.Put(h)

	bufPtr := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufPtr)

	if _, err := io.CopyBuffer(h, file, *bufPtr); err != nil {
		return false
	}

	var actual [md5.Size]byte
	h.Sum(actual[:0])

	return actual == expected
}

func getFreshURL(publicKey, remotePath string) (string, error) {
	pauseMu.Lock()
	if time.Now().Before(globalPauseUntil) {
		sleepDuration := time.Until(globalPauseUntil)
		pauseMu.Unlock()

		log.Printf("⏸️ Timeout for all threads enabled (429). Waiting %v...", sleepDuration.Truncate(time.Second))
		time.Sleep(sleepDuration)
	} else {
		pauseMu.Unlock()
	}

	apiLimiter.Wait(context.Background())

	baseURL := "https://cloud-api.yandex.net/v1/disk/public/resources/download"
	params := url.Values{}
	params.Add("public_key", publicKey)
	params.Add("path", remotePath)

	resp, err := client.Get(baseURL + "?" + params.Encode())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		backoff := 30 * time.Second

		pauseMu.Lock()
		globalPauseUntil = time.Now().Add(backoff)
		pauseMu.Unlock()

		log.Printf("🚨 YANDEX IS CHOKING (429)! Enabling timeout %v for all threads!", backoff)
		return "", fmt.Errorf("API error: 429 TOO MANY REQUESTS")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: %s", resp.Status)
	}

	var res struct {
		Href string `json:"href"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	return res.Href, nil
}
