package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type AriaClient struct {
	conn               *websocket.Conn
	tasksMu            sync.RWMutex
	gidToTask          map[string]DownloadTask
	writeMu            sync.Mutex
	pendingTasks       map[string]DownloadTask
	OnDownloadStart    func(task DownloadTask, gid string)
	OnDownloadComplete func(task DownloadTask, gid string)
	OnDownloadError    func(task DownloadTask, gid string)
	OnStatsUpdate      func(speedMB float64, active, waiting int)
}

func NewAriaClient(wsURL string) (*AriaClient, error) {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, err
	}

	client := &AriaClient{
		conn:         conn,
		gidToTask:    make(map[string]DownloadTask),
		pendingTasks: make(map[string]DownloadTask),
	}

	go client.listenLoop()
	go client.startStatPoller()

	return client, nil
}

func (c *AriaClient) startStatPoller() {
	ticker := time.NewTicker(500 * time.Millisecond)
	for range ticker.C {
		req := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "global_stat",
			"method":  "aria2.getGlobalStat",
			"params":  []interface{}{},
		}
		c.sendJSON(req)
	}
}

func (c *AriaClient) listenLoop() {
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("❌ Socket closed: %v\n", err)
			return
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(message, &raw); err != nil {
			log.Printf("⚠️ Broken JSON from Aria: %v\n", err)
			continue
		}

		if errObj, hasErr := raw["error"]; hasErr {
			log.Printf("🔥 ARIA REJECTED JOB! Response: %v\n", errObj)
			continue
		}

		id, ok := raw["id"].(string)

		if ok && id == "global_stat" {
			if res, ok := raw["result"].(map[string]interface{}); ok {
				speedStr, _ := res["downloadSpeed"].(string)
				activeStr, _ := res["numActive"].(string)
				waitingStr, _ := res["numWaiting"].(string)

				speedBytes, _ := strconv.ParseFloat(speedStr, 64)
				active, _ := strconv.Atoi(activeStr)
				waiting, _ := strconv.Atoi(waitingStr)

				speedMB := speedBytes / 1024 / 1024
				if c.OnStatsUpdate != nil {
					c.OnStatsUpdate(speedMB, active, waiting)
				}
			}
			continue
		}

		if ok && id != "" {
			if gid, ok := raw["result"].(string); ok {
				// log.Printf("📥 Aria accepted job: %s (GID: %s)", id, gid)

				c.tasksMu.Lock()
				if originalTask, exists := c.pendingTasks[id]; exists {
					c.gidToTask[gid] = originalTask
					delete(c.pendingTasks, id)
				} else {
					c.gidToTask[gid] = DownloadTask{SavePath: id}
				}
				c.tasksMu.Unlock()
			}
			continue
		}

		method, _ := raw["method"].(string)
		params, _ := raw["params"].([]interface{})
		if len(params) == 0 {
			continue
		}

		firstParam, _ := params[0].(map[string]interface{})
		gid, _ := firstParam["gid"].(string)

		c.tasksMu.RLock()
		task := c.gidToTask[gid]
		c.tasksMu.RUnlock()

		switch method {
		case "aria2.onDownloadComplete":
			log.Printf("✅ Downloaded: %s\n", filepath.Base(task.SavePath))
			if c.OnDownloadComplete != nil {
				c.OnDownloadComplete(task, gid)
			}
		case "aria2.onDownloadError":
			log.Printf("❌ Error while downloading: %s (GID: %s)\n", filepath.Base(task.SavePath), gid)
			if c.OnDownloadError != nil {
				c.OnDownloadError(task, gid)
			}
		case "aria2.onDownloadStart":
			if c.OnDownloadStart != nil {
				c.OnDownloadStart(task, gid)
			}
		}
	}
}

func (c *AriaClient) AddTask(task DownloadTask) error {
	reqID := task.SavePath

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "aria2.addUri",
		"params": []interface{}{
			[]string{task.URL},
			map[string]string{
				"dir": filepath.Dir(task.SavePath),
				"out": filepath.Base(task.SavePath),

				"allow-overwrite":    "true",
				"auto-file-renaming": "false",
			},
		},
	}

	c.tasksMu.Lock()
	c.pendingTasks[reqID] = task
	c.tasksMu.Unlock()

	return c.sendJSON(req)
}

func startAriaDaemon() (*exec.Cmd, error) {
	ariaPath, err := getAriaExecutable()
	if err != nil {
		return nil, fmt.Errorf("Couldn't find aria2c bin: %w", err)
	}

	args := []string{
		"--enable-rpc=true",             // JSON-RPC
		"--rpc-listen-port=6800",        // WS/HTTP port
		"--rpc-listen-all=false",        // Listen only localhost (Security)
		"--max-concurrent-downloads=8",  // 8 files concurrently
		"--max-connection-per-server=8", // 8 sockets per file
		"--split=8",                     // Slice file for 8 slices
		"--min-split-size=1M",           // Minimum size 1 MB
		"-c",                            // Enable continue
		"--file-allocation=none",        // No drive allocate
		"--console-log-level=warn",      // No log
	}

	cmd := exec.Command(ariaPath, args...)

	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("Error while starting aria2c: %w", err)
	}

	time.Sleep(200 * time.Millisecond)

	return cmd, nil
}

func (c *AriaClient) sendJSON(v interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}
