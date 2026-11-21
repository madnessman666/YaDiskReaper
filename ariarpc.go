package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// AriaStats is a lightweight snapshot of aria2's global progress.
type AriaStats struct {
	SpeedBytes  float64
	Active      int
	Waiting     int
	ActiveBytes int64 // bytes already downloaded by currently active jobs
}

type AriaClient struct {
	conn               *websocket.Conn
	tasksMu            sync.RWMutex
	gidToTask          map[string]DownloadTask
	writeMu            sync.Mutex
	pendingTasks       map[string]DownloadTask
	statsMu            sync.Mutex
	stats              AriaStats
	OnDownloadStart    func(task DownloadTask, gid string)
	OnDownloadComplete func(task DownloadTask, gid string)
	OnDownloadError    func(task DownloadTask, gid string)
	OnStatsUpdate      func(stats AriaStats)
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

// connectAria retries the initial RPC connection: aria2 may need a moment to
// open its port, especially on the very first run while the binary is extracted.
func connectAria(wsURL string) (*AriaClient, error) {
	deadline := time.Now().Add(5 * time.Second)

	var lastErr error
	for time.Now().Before(deadline) {
		client, err := NewAriaClient(wsURL)
		if err == nil {
			return client, nil
		}
		lastErr = err
		time.Sleep(150 * time.Millisecond)
	}

	return nil, lastErr
}

func (c *AriaClient) startStatPoller() {
	ticker := time.NewTicker(500 * time.Millisecond)
	for range ticker.C {
		c.sendJSON(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "global_stat",
			"method":  "aria2.getGlobalStat",
			"params":  []interface{}{},
		})
		c.sendJSON(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "active_stat",
			"method":  "aria2.tellActive",
			"params":  []interface{}{},
		})
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
			if id, ok := raw["id"].(string); ok {
				c.tasksMu.Lock()
				failedTask, hasTask := c.pendingTasks[id]
				delete(c.pendingTasks, id)
				c.tasksMu.Unlock()

				if hasTask && c.OnDownloadError != nil {
					c.OnDownloadError(failedTask, "")
				}
			}
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

				c.statsMu.Lock()
				c.stats.SpeedBytes = speedBytes
				c.stats.Active = active
				c.stats.Waiting = waiting
				c.statsMu.Unlock()

				c.publishStats()
			}
			continue
		}

		if ok && id == "active_stat" {
			if items, ok := raw["result"].([]interface{}); ok {
				var completed int64
				for _, item := range items {
					status, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					completedStr, _ := status["completedLength"].(string)
					value, _ := strconv.ParseInt(completedStr, 10, 64)
					completed += value
				}

				c.statsMu.Lock()
				c.stats.ActiveBytes = completed
				c.statsMu.Unlock()

				c.publishStats()
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

		c.tasksMu.Lock()
		task, exists := c.gidToTask[gid]
		// if exists {
		// 	delete(c.gidToTask, gid)
		// }
		c.tasksMu.Unlock()

		if !exists {
			continue
		}

		switch method {
		case "aria2.onDownloadComplete":
			log.Printf("✅ Downloaded: %s\n", filepath.Base(task.SavePath))
			c.tasksMu.Lock()
			delete(c.gidToTask, gid)
			c.tasksMu.Unlock()

			if c.OnDownloadComplete != nil {
				c.OnDownloadComplete(task, gid)
			}
		case "aria2.onDownloadError":
			log.Printf("❌ Error while downloading: %s (GID: %s)\n", filepath.Base(task.SavePath), gid)
			c.tasksMu.Lock()
			delete(c.gidToTask, gid)
			c.tasksMu.Unlock()

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
	absPath, err := filepath.Abs(task.SavePath)
	if err != nil {
		return err
	}

	reqID := task.SavePath

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "aria2.addUri",
		"params": []interface{}{
			[]string{task.URL},
			map[string]string{
				"dir": filepath.Dir(absPath),
				"out": filepath.Base(absPath),

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

func startAriaDaemon(ariaPort int) (*exec.Cmd, error) {
	ariaPath, err := getAriaExecutable()
	if err != nil {
		return nil, fmt.Errorf("Couldn't find aria2c bin: %w", err)
	}

	args := []string{
		"--enable-rpc=true",      // JSON-RPC
		"--rpc-listen-all=false", // Listen only localhost (Security)
		fmt.Sprintf("--max-concurrent-downloads=%d", parallel),  // 8 files concurrently
		fmt.Sprintf("--split=%d", parallel),                     // 8 sockets per file
		fmt.Sprintf("--max-connection-per-server=%d", parallel), // Slice file for 8 slices
		"--min-split-size=1M",                                   // Minimum size 1 MB
		"-c",                                                    // Enable continue
		"--file-allocation=none",                                // No drive allocate
		"--console-log-level=warn",                              // No log
		fmt.Sprintf("--stop-with-process=%d", os.Getpid()),      // Stop aria2c when yadiskreaper exits
		fmt.Sprintf("--rpc-listen-port=%d", ariaPort),           //Set port for RPC interface
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

func (c *AriaClient) publishStats() {
	c.statsMu.Lock()
	stats := c.stats
	c.statsMu.Unlock()

	if c.OnStatsUpdate != nil {
		c.OnStatsUpdate(stats)
	}
}
