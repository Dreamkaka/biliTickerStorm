package master

import (
	. "biliTickerStorm/internal/common"
	"biliTickerStorm/internal/common/workercfg"
	"biliTickerStorm/internal/master/ticketcfg"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxEvents = 200

type EventRecord struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

type WorkerView struct {
	WorkerID     string    `json:"worker_id"`
	Address      string    `json:"address"`
	Status       string    `json:"status"`
	TaskAssigned string    `json:"task_assigned"`
	UpdateTime   time.Time `json:"update_time"`
	BanTime      time.Time `json:"ban_time,omitempty"`
	BanRemainSec int64     `json:"ban_remain_sec,omitempty"`
	StatusDetail string    `json:"status_detail,omitempty"`
	ProxyLabel   string    `json:"proxy_label,omitempty"`
}

type TaskView struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	AssignedTo string    `json:"assigned_to"`
	TaskName   string    `json:"task_name"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	RetryCount int       `json:"retry_count"`
	Preview    string    `json:"preview,omitempty"`
}

type Overview struct {
	Workers struct {
		Total   int `json:"total"`
		Idle    int `json:"idle"`
		Working int `json:"working"`
		Risking int `json:"risking"`
		Down    int `json:"down"`
	} `json:"workers"`
	Tasks struct {
		Total   int `json:"total"`
		Pending int `json:"pending"`
		Doing   int `json:"doing"`
		Done    int `json:"done"`
	} `json:"tasks"`
	ConfigPath string    `json:"config_path"`
	ServerTime time.Time `json:"server_time"`
}

type ConfigFileView struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	HasTask   bool      `json:"has_task"`
	TaskID    string    `json:"task_id,omitempty"`
	TaskStatus string   `json:"task_status,omitempty"`
}

func (s *Server) initEvents() {
	s.events = make([]EventRecord, 0, maxEvents)
}

func (s *Server) PushEvent(level, message string) {
	s.eventsMux.Lock()
	defer s.eventsMux.Unlock()
	if s.events == nil {
		s.events = make([]EventRecord, 0, maxEvents)
	}
	s.events = append(s.events, EventRecord{
		Time:    time.Now(),
		Level:   level,
		Message: message,
	})
	if len(s.events) > maxEvents {
		s.events = s.events[len(s.events)-maxEvents:]
	}
}

func (s *Server) ListEvents(limit int) []EventRecord {
	s.eventsMux.RLock()
	defer s.eventsMux.RUnlock()
	if limit <= 0 || limit > len(s.events) {
		limit = len(s.events)
	}
	if limit == 0 {
		return []EventRecord{}
	}
	start := len(s.events) - limit
	out := make([]EventRecord, limit)
	copy(out, s.events[start:])
	return out
}

func (s *Server) SnapshotOverview() Overview {
	s.workersMux.RLock()
	s.tasksMux.RLock()
	defer s.workersMux.RUnlock()
	defer s.tasksMux.RUnlock()

	var ov Overview
	ov.ConfigPath = Cfg.Configpath
	ov.ServerTime = time.Now()
	for _, w := range s.workers {
		ov.Workers.Total++
		switch w.Status {
		case Idle:
			ov.Workers.Idle++
		case Working:
			ov.Workers.Working++
		case Risking:
			ov.Workers.Risking++
		case Down:
			ov.Workers.Down++
		}
	}
	for _, t := range s.tasks {
		ov.Tasks.Total++
		switch t.Status {
		case TaskStatusPending:
			ov.Tasks.Pending++
		case TaskStatusDoing:
			ov.Tasks.Doing++
		case TaskStatusDone:
			ov.Tasks.Done++
		}
	}
	return ov
}

func (s *Server) ListWorkersView() []WorkerView {
	s.workersMux.RLock()
	defer s.workersMux.RUnlock()
	now := time.Now()
	out := make([]WorkerView, 0, len(s.workers))
	for _, w := range s.workers {
		v := WorkerView{
			WorkerID:     w.WorkerID,
			Address:      w.Address,
			Status:       w.Status.String(),
			TaskAssigned: w.TaskAssigned,
			UpdateTime:   w.UpdateTime,
			BanTime:      w.BanTime,
			StatusDetail: w.StatusDetail,
			ProxyLabel:   w.ProxyLabel,
		}
		if w.Status == Risking && !w.BanTime.IsZero() {
			remain := s.banTimeout - now.Sub(w.BanTime)
			if remain > 0 {
				v.BanRemainSec = int64(remain.Seconds())
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkerID < out[j].WorkerID })
	return out
}

func (s *Server) ListTasksView() []TaskView {
	s.tasksMux.RLock()
	defer s.tasksMux.RUnlock()
	out := make([]TaskView, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, TaskView{
			ID:         t.ID,
			Status:     string(t.Status),
			AssignedTo: t.AssignedTo,
			TaskName:   t.TaskName,
			CreatedAt:  t.CreatedAt,
			UpdatedAt:  t.UpdatedAt,
			RetryCount: t.RetryCount,
			Preview:    previewConfig(t.TickerConfigContent),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func previewConfig(content string) string {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if v, ok := raw["username"].(string); ok && v != "" {
		parts = append(parts, v)
	}
	if v, ok := raw["detail"].(string); ok && v != "" {
		parts = append(parts, v)
	} else {
		if pid, ok := raw["project_id"]; ok {
			parts = append(parts, fmt.Sprintf("project=%v", pid))
		}
		if sid, ok := raw["sku_id"]; ok {
			parts = append(parts, fmt.Sprintf("sku=%v", sid))
		}
	}
	if ss, ok := raw["sale_start"]; ok && ss != nil && ss != "" {
		parts = append(parts, fmt.Sprintf("起售=%v", ss))
	}
	return strings.Join(parts, " · ")
}

func (s *Server) GetTaskConfig(taskID string) (string, error) {
	s.tasksMux.RLock()
	defer s.tasksMux.RUnlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return "", fmt.Errorf("task not found")
	}
	return t.TickerConfigContent, nil
}

// sanitizeTaskName 对齐 biliTickerBuy filename_filter：去掉 \ / : * ? " < > |，保留中文
func sanitizeTaskName(name string) string {
	return ticketcfg.FilenameFilter(name)
}

// reservedConfigBasenames 与抢票任务 JSON 共存于 CONFIG_PATH，加载任务时必须跳过。
var reservedConfigBasenames = map[string]struct{}{
	"worker_settings": {},
}

// isReservedConfigFile 判断 CONFIG_PATH 下文件名是否为系统/集群配置（非抢票任务）。
func isReservedConfigFile(filename string) bool {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	base = strings.ToLower(strings.TrimSpace(base))
	_, ok := reservedConfigBasenames[base]
	return ok
}

// AddTaskFromJSONBytes 原样写入 JSON 字节（保持 Buy 字段顺序与 cookies 形态）
func (s *Server) AddTaskFromJSONBytes(name string, content []byte, writeFile bool) (*TaskInfo, error) {
	name = sanitizeTaskName(name)
	if name == "" {
		name = fmt.Sprintf("task-%d", time.Now().Unix())
	}
	if isReservedConfigFile(name + ".json") {
		return nil, fmt.Errorf("名称 %q 为系统保留配置，不能作为抢票任务", name)
	}
	var probe map[string]interface{}
	if err := json.Unmarshal(content, &probe); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	text := string(content)
	if writeFile {
		path := filepath.Join(Cfg.Configpath, name+".json")
		if err := os.MkdirAll(Cfg.Configpath, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			return nil, err
		}
	}
	return s.addTaskLocked(name, text)
}

func (s *Server) AddTaskFromJSON(name, content string, writeFile bool) (*TaskInfo, error) {
	// 上传的 Buy 文件：原样保存，绝不改写字段/cookies
	return s.AddTaskFromJSONBytes(name, []byte(content), writeFile)
}

func (s *Server) addTaskLocked(name, content string) (*TaskInfo, error) {
	// 同名任务：若已存在且未 Done，更新内容并 requeue；Done 则新建
	s.tasksMux.Lock()
	for _, t := range s.tasks {
		if t.TaskName == name && t.Status != TaskStatusDone {
			t.TickerConfigContent = content
			t.Status = TaskStatusPending
			t.AssignedTo = ""
			t.UpdatedAt = time.Now()
			s.tasksMux.Unlock()
			s.PushEvent("info", fmt.Sprintf("更新任务配置并重新入队: %s (%s)", name, t.ID))
			s.triggerSchedule()
			return t, nil
		}
	}
	s.tasksMux.Unlock()

	task := s.CreateTask(name, content)
	s.PushEvent("info", fmt.Sprintf("创建任务: %s (%s)", name, task.ID))
	return task, nil
}

func (s *Server) DeleteTask(taskID string, deleteFile bool) error {
	s.workersMux.Lock()
	s.tasksMux.Lock()
	defer s.workersMux.Unlock()
	defer s.tasksMux.Unlock()

	t, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task not found")
	}
	name := t.TaskName
	if t.AssignedTo != "" {
		if w, ok := s.workers[t.AssignedTo]; ok {
			w.TaskAssigned = ""
			if w.Status == Working {
				w.Status = Idle
			}
		}
	}
	delete(s.tasks, taskID)
	s.PushEvent("info", fmt.Sprintf("删除任务: %s (%s)", name, taskID))
	s.triggerSchedule()

	if deleteFile && name != "" && !isReservedConfigFile(name+".json") {
		path := filepath.Join(Cfg.Configpath, name+".json")
		_ = os.Remove(path)
	}
	return nil
}

func (s *Server) RequeueTask(taskID string) error {
	s.tasksMux.Lock()
	defer s.tasksMux.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task not found")
	}
	s.clearAndPendingTask(t)
	s.PushEvent("info", fmt.Sprintf("任务重新入队: %s (%s)", t.TaskName, taskID))
	s.triggerSchedule()
	return nil
}

func (s *Server) ReloadTasksFromDir() (int, error) {
	files, err := os.ReadDir(Cfg.Configpath)
	if err != nil {
		return 0, err
	}
	added := 0
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		if isReservedConfigFile(file.Name()) {
			continue
		}
		fullPath := filepath.Join(Cfg.Configpath, file.Name())
		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		taskName := strings.TrimSuffix(file.Name(), ".json")
		s.tasksMux.RLock()
		exists := false
		for _, t := range s.tasks {
			if t.TaskName == taskName {
				exists = true
				break
			}
		}
		s.tasksMux.RUnlock()
		if exists {
			continue
		}
		s.CreateTask(taskName, string(content))
		added++
	}
	s.PushEvent("info", fmt.Sprintf("从目录重载，新增 %d 个任务", added))
	return added, nil
}

func (s *Server) ListConfigFiles() ([]ConfigFileView, error) {
	entries, err := os.ReadDir(Cfg.Configpath)
	if err != nil {
		if os.IsNotExist(err) {
			return []ConfigFileView{}, nil
		}
		return nil, err
	}
	s.tasksMux.RLock()
	byName := make(map[string]*TaskInfo)
	for _, t := range s.tasks {
		byName[t.TaskName] = t
	}
	s.tasksMux.RUnlock()

	out := make([]ConfigFileView, 0)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if isReservedConfigFile(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		v := ConfigFileView{
			Name:    name,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if t, ok := byName[name]; ok {
			v.HasTask = true
			v.TaskID = t.ID
			v.TaskStatus = string(t.Status)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

func (s *Server) ReadConfigFile(name string) (string, error) {
	name = sanitizeTaskName(name)
	if name == "" {
		return "", fmt.Errorf("invalid name")
	}
	if isReservedConfigFile(name + ".json") {
		return "", fmt.Errorf("reserved config")
	}
	path := filepath.Join(Cfg.Configpath, name+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Server) RuntimeInfo() map[string]interface{} {
	s.workersMux.RLock()
	s.tasksMux.RLock()
	defer s.workersMux.RUnlock()
	defer s.tasksMux.RUnlock()
	return map[string]interface{}{
		"version":          Version,
		"build_time":       BuildTime,
		"git_commit":       GitCommit,
		"config_path":      Cfg.Configpath,
		"web_addr":         Cfg.WebAddr,
		"web_auth_enabled": Cfg.WebToken != "",
		"heartbeat_timeout_sec": int(s.heartbeatTimeout.Seconds()),
		"task_timeout_sec":      int(s.taskTimeout.Seconds()),
		"ban_timeout_sec":       int(s.banTimeout.Seconds()),
		"max_retries":           s.maxRetries,
		"grpc_port":             40052,
		"features": map[string]bool{
			"cluster_dashboard": true,
			"task_crud":         true,
			"qr_login":          true,
			"config_generate":   true,
			"event_log":         true,
			"pushplus":          true,
			"proxy_pool":        true,
			"h2_ja3":            false,
			"multi_notify":      true,
			"worker_settings":   true,
			"payment_qr":        false,
			"page_gate":         false,
			"local_audio":       false,
		},
	}
}

// GetWorkerSettings 脱敏视图 + version。
func (s *Server) GetWorkerSettings() (workercfg.Settings, int64) {
	if s.workerSettings == nil {
		return workercfg.Settings{}, 0
	}
	return s.workerSettings.GetMasked()
}

// GetWorkerSettingsFull 完整密钥（仅服务端合并用，勿直接返回前端）。
func (s *Server) GetWorkerSettingsFull() (workercfg.Settings, int64) {
	if s.workerSettings == nil {
		return workercfg.Settings{}, 0
	}
	return s.workerSettings.Get()
}

// SaveWorkerSettings 写入磁盘并更新缓存。
func (s *Server) SaveWorkerSettings(cfg workercfg.Settings) (int64, error) {
	if s.workerSettings == nil {
		return 0, fmt.Errorf("settings store not ready")
	}
	ver, err := s.workerSettings.Save(cfg)
	if err != nil {
		return 0, err
	}
	s.PushEvent("info", fmt.Sprintf("Worker 设置已更新 version=%d", ver))
	return ver, nil
}

// ExportWorkerSettingsEnv 导出为 .env 片段（完整密钥，仅导出用）。
func (s *Server) ExportWorkerSettingsEnv() string {
	if s.workerSettings == nil {
		return ""
	}
	cfg, _ := s.workerSettings.Get()
	var b strings.Builder
	write := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	writeInt := func(k string, p *int) {
		if p == nil {
			return
		}
		b.WriteString(fmt.Sprintf("%s=%d\n", k, *p))
	}
	writeBool := func(k string, p *bool) {
		if p == nil {
			return
		}
		b.WriteString(fmt.Sprintf("%s=%v\n", k, *p))
	}
	write("PUSHPLUS_TOKEN", cfg.PushplusToken)
	write("BARK_TOKEN", cfg.BarkToken)
	write("SERVERCHAN_KEY", cfg.ServerChanKey)
	write("SERVERCHAN3_API_URL", cfg.ServerChan3APIURL)
	write("TELEGRAM_BOT_TOKEN", cfg.TelegramBotToken)
	write("TELEGRAM_CHAT_ID", cfg.TelegramChatID)
	write("TELEGRAM_HTTP_PROXY", cfg.TelegramHTTPProxy)
	writeInt("TICKET_INTERVAL", cfg.Interval)
	writeInt("RATE_LIMIT_DELAY_MS", cfg.RateLimitDelayMs)
	writeInt("RISK_LOCAL_RETRIES", cfg.RiskLocalRetries)
	writeInt("RISK_COOLDOWN_BASE_MS", cfg.RiskCooldownBaseMs)
	writeInt("RISK_COOLDOWN_MAX_SEC", cfg.RiskCooldownMaxSec)
	write("PROXY_LIST", cfg.ProxyList)
	writeInt("PROXY_MAX_FAILS", cfg.ProxyMaxFails)
	writeInt("PROXY_COOLDOWN_SEC", cfg.ProxyCooldownSec)
	writeInt("PROXY_MAX_BACKOFF_SEC", cfg.ProxyMaxBackoffSec)
	write("PROXY_API_URL", cfg.ProxyAPIURL)
	writeInt("PROXY_API_COUNT", cfg.ProxyAPICount)
	write("PROXY_API_SCHEME", cfg.ProxyAPIScheme)
	writeInt("CONN_PER_HOST", cfg.ConnPerHost)
	writeInt("CREATE_BATCH_SIZE", cfg.CreateBatchSize)
	writeBool("ENABLE_WARMUP", cfg.EnableWarmup)
	write("TICKET_TIME_START", cfg.TicketTimeStart)
	return b.String()
}

func (s *Server) ClearEvents() {
	s.eventsMux.Lock()
	defer s.eventsMux.Unlock()
	s.events = s.events[:0]
}

func (s *Server) MaskedConfigJSON(content string) (string, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return "", err
	}
	maskSensitiveMap(raw)
	b, err := json.MarshalIndent(raw, "", "  ")
	return string(b), err
}

func maskSensitiveMap(raw map[string]interface{}) {
	sensitiveKeys := map[string]bool{
		"cookies": true, "cookie": true, "token": true, "csrf": true,
		"bili_jct": true, "SESSDATA": true, "access_key": true,
	}
	for k, v := range raw {
		if sensitiveKeys[k] || strings.Contains(strings.ToLower(k), "cookie") {
			switch c := v.(type) {
			case []interface{}:
				raw[k] = fmt.Sprintf("[%d items masked]", len(c))
			case string:
				if len(c) > 8 {
					raw[k] = c[:4] + "…[masked]"
				} else {
					raw[k] = "[masked]"
				}
			default:
				raw[k] = "[masked]"
			}
			continue
		}
		switch t := v.(type) {
		case map[string]interface{}:
			// 购票人/地址字段脱敏
			for _, field := range []string{"personal_id", "id_card_front", "id_card_back", "tel", "phone"} {
				if _, ok := t[field]; ok {
					t[field] = "[masked]"
				}
			}
			maskSensitiveMap(t)
		case []interface{}:
			for _, item := range t {
				if m, ok := item.(map[string]interface{}); ok {
					for _, field := range []string{"personal_id", "id_card_front", "id_card_back", "tel", "phone"} {
						if _, ok := m[field]; ok {
							m[field] = "[masked]"
						}
					}
					// cookie 条目
					if name, _ := m["name"].(string); name != "" {
						if _, ok := m["value"]; ok {
							m["value"] = "[masked]"
						}
					}
					maskSensitiveMap(m)
				}
			}
		}
	}
}

