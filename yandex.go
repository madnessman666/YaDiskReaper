package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

var visitedPaths = make(map[string]bool)

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
			tasks = append(tasks, DownloadTask{URL: res.File, SavePath: filepath.Join(localDir, res.Name), ExpectedMD5: res.MD5})
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
					tasks = append(tasks, DownloadTask{URL: item.File, SavePath: currLocal, ExpectedMD5: item.MD5})
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
	if expectedMD5 == "" {
		return false
	}

	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false
	}

	calculatedMD5 := hex.EncodeToString(hash.Sum(nil))
	return calculatedMD5 == expectedMD5
}
