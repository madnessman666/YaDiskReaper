package main

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strconv"
)

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
			tasks = append(tasks, DownloadTask{URL: res.File, SavePath: filepath.Join(localDir, res.Name)})
			return tasks, res.Name, nil
		}

		if res.Embedded != nil {
			for _, item := range res.Embedded.Items {
				currLocal := filepath.Join(localDir, item.Name)
				if item.Type == "dir" {
					sub, _, _ := getAllTasks(publicKey, item.Path, currLocal)
					tasks = append(tasks, sub...)
				} else {
					tasks = append(tasks, DownloadTask{URL: item.File, SavePath: currLocal})
				}
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
