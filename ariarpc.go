package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type AriaClient struct {
	conn      *websocket.Conn
	tasksMu   sync.RWMutex
	gidToTask map[string]DownloadTask

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
		conn:      conn,
		gidToTask: make(map[string]DownloadTask),
	}

	go client.listenLoop()
	go client.startStatPoller()

	return client, nil
}

func (c *AriaClient) startStatPoller() {
	ticker := time.NewTicker(500 * time.Millisecond)
	for range ticker.C {
		// Шлем легкий запрос в сокет
		req := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "global_stat", // Метка, чтобы узнать ответ!
			"method":  "aria2.getGlobalStat",
			"params":  []interface{}{},
		}
		c.conn.WriteJSON(req)
	}
}

func (c *AriaClient) listenLoop() {
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var raw map[string]interface{}
		json.Unmarshal(message, &raw)
		id, ok := raw["id"].(string)

		if ok && id == "global_stat" {
			if res, ok := raw["result"].(map[string]interface{}); ok {
				speedBytes, _ := strconv.ParseFloat(res["downloadSpeed"].(string), 64)
				active, _ := strconv.Atoi(res["numActive"].(string))
				waiting, _ := strconv.Atoi(res["numWaiting"].(string))

				speedMB := speedBytes / 1024 / 1024
				if c.OnStatsUpdate != nil {
					c.OnStatsUpdate(speedMB, active, waiting)
				}
			}
			continue
		}

		if ok && id != "" {
			if gid, ok := raw["result"].(string); ok {
				c.tasksMu.Lock()
				c.gidToTask[gid] = DownloadTask{SavePath: id}
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
			if c.OnDownloadComplete != nil {
				c.OnDownloadComplete(task, gid)
			}
		case "aria2.onDownloadError":
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
	reqID := task.SavePath // Используем путь как уникальный ID запроса

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

	return c.conn.WriteJSON(req)
}

func startAriaDaemon() (*exec.Cmd, error) {
	ariaPath, err := getAriaExecutable()
	if err != nil {
		return nil, fmt.Errorf("не нашел бинарник aria2c: %w", err)
	}

	args := []string{
		"--enable-rpc=true",             // Врубаем JSON-RPC
		"--rpc-listen-port=6800",        // Порт вебсокета/HTTP
		"--rpc-listen-all=false",        // Слушать только localhost (безопасность)
		"--max-concurrent-downloads=8",  // 8 файлов одновременно
		"--max-connection-per-server=8", // 8 сокетов на один файл (всего 64 сокета!)
		"--split=8",                     // Резать файл на 8 кусков
		"--min-split-size=1M",           // Минимальный размер куска 1 МБ
		"-c",                            // Врубаем докачку (continue)
		"--file-allocation=none",        // Не тратить время на разметку диска
		"--console-log-level=warn",      // Не срать мусором в лог
	}

	cmd := exec.Command(ariaPath, args...)

	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("ошибка запуска процесса aria2c: %w", err)
	}

	time.Sleep(200 * time.Millisecond)

	return cmd, nil
}
