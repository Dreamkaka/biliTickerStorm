package web

import (
	"biliTickerStorm/internal/master"
	"biliTickerStorm/internal/master/biliapi"
	"biliTickerStorm/internal/master/ticketcfg"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed all:static
var staticFS embed.FS

type Server struct {
	master   *master.Server
	token    string
	mux      *http.ServeMux
	bili     *biliapi.Client
	accounts *biliapi.AccountStore
}

func New(m *master.Server, token string, configPath string) *Server {
	accDir := filepath.Join(configPath, "accounts")
	s := &Server{
		master:   m,
		token:    token,
		mux:      http.NewServeMux(),
		bili:     biliapi.NewClient(),
		accounts: biliapi.NewAccountStore(accDir),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.cors(s.auth(s.mux))
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/v1/overview", s.handleOverview)
	s.mux.HandleFunc("/api/v1/workers", s.handleWorkers)
	s.mux.HandleFunc("/api/v1/tasks", s.handleTasks)
	s.mux.HandleFunc("/api/v1/tasks/", s.handleTaskSub)
	s.mux.HandleFunc("/api/v1/events", s.handleEvents)
	s.mux.HandleFunc("/api/v1/events/clear", s.handleEventsClear)
	s.mux.HandleFunc("/api/v1/meta", s.handleMeta)
	s.mux.HandleFunc("/api/v1/settings/worker", s.handleWorkerSettings)
	s.mux.HandleFunc("/api/v1/settings/worker/export", s.handleWorkerSettingsExport)
	s.mux.HandleFunc("/api/v1/configs", s.handleConfigs)
	s.mux.HandleFunc("/api/v1/configs/", s.handleConfigSub)
	s.mux.HandleFunc("/api/v1/auth/qr/start", s.handleQRStart)
	s.mux.HandleFunc("/api/v1/auth/qr/poll", s.handleQRPoll)
	s.mux.HandleFunc("/api/v1/auth/accounts", s.handleAccounts)
	s.mux.HandleFunc("/api/v1/auth/accounts/", s.handleAccountSub)
	s.mux.HandleFunc("/api/v1/project/", s.handleProject)
	s.mux.HandleFunc("/api/v1/configs/generate", s.handleGenerateConfig)
	s.mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": master.Version,
		})
	})

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		// SPA fallback
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "." {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" || !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		token := r.URL.Query().Get("token")
		if strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
		if token != s.token {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	info := s.master.RuntimeInfo()
	info["accounts_count"] = len(s.accounts.List())
	info["active_account"] = s.accounts.ActiveUID()
	info["release_url"] = "https://github.com/mikumifa/biliTickerStorm/releases"
	info["repo_url"] = "https://github.com/mikumifa/biliTickerStorm"
	info["buy_repo_url"] = "https://github.com/mikumifa/biliTickerBuy"
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleWorkerSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, ver := s.master.GetWorkerSettings()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"settings": cfg,
			"version":  ver,
			"path":     "worker_settings.json",
			"note":     "空字段表示不覆盖 worker 环境变量；MASTER_SERVER_ADDR 不可在此配置。密钥字段脱敏显示，保存时留空或含****则保留原值。",
		})
	case http.MethodPut, http.MethodPost:
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// 支持 {"settings":{...}} 或直接 {...}
		var wrapped struct {
			Settings json.RawMessage `json:"settings"`
		}
		settingsRaw := raw
		if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Settings) > 0 {
			settingsRaw = wrapped.Settings
		}
		cfg, err := decodeWorkerSettingsJSON(settingsRaw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		cfg = mergePreserveSecrets(s, cfg)
		ver, err := s.master.SaveWorkerSettings(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		masked, _ := s.master.GetWorkerSettings()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":       true,
			"version":  ver,
			"settings": masked,
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleWorkerSettingsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	envText := s.master.ExportWorkerSettingsEnv()
	writeJSON(w, http.StatusOK, map[string]string{"env": envText})
}

func (s *Server) handleEventsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.master.ClearEvents()
	s.master.PushEvent("info", "事件日志已清空")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, s.master.SnapshotOverview())
}

func (s *Server) handleWorkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, s.master.ListWorkersView())
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.master.ListTasksView())
	case http.MethodPost:
		s.createTask(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		files := r.MultipartForm.File["files"]
		if len(files) == 0 {
			files = r.MultipartForm.File["file"]
		}
		created := make([]interface{}, 0)
		for _, fh := range files {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			b, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				continue
			}
			name := strings.TrimSuffix(fh.Filename, ".json")
			task, err := s.master.AddTaskFromJSON(name, string(b), true)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			created = append(created, map[string]string{"id": task.ID, "name": task.TaskName})
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"created": created})
		return
	}

	var body struct {
		Name    string          `json:"name"`
		Content json.RawMessage `json:"content"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	content := string(body.Content)
	if content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content required"})
		return
	}
	// content 可能是对象被 RawMessage 包了一层
	task, err := s.master.AddTaskFromJSON(body.Name, content, true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": task.ID, "name": task.TaskName})
}

func (s *Server) handleTaskSub(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if parts[0] == "reload" && r.Method == http.MethodPost {
		n, err := s.master.ReloadTasksFromDir()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"added": n})
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			raw, err := s.master.GetTaskConfig(id)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			masked, err := s.master.MaskedConfigJSON(raw)
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]string{"content": raw})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"content": masked})
		case http.MethodDelete:
			deleteFile := r.URL.Query().Get("delete_file") == "1" || r.URL.Query().Get("delete_file") == "true"
			if err := s.master.DeleteTask(id, deleteFile); err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	if parts[1] == "requeue" && r.Method == http.MethodPost {
		if err := s.master.RequeueTask(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, s.master.ListEvents(limit))
}

func (s *Server) handleConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	list, err := s.master.ListConfigFiles()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleConfigSub(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/configs/")
	if name == "generate" {
		s.handleGenerateConfig(w, r)
		return
	}
	if name == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	raw, err := s.master.ReadConfigFile(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	masked, err := s.master.MaskedConfigJSON(raw)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"name": name, "content": raw})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "content": masked})
}

func (s *Server) handleQRStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	res, err := s.bili.StartQRLogin()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleQRPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	key := r.URL.Query().Get("qrcode_key")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "qrcode_key required"})
		return
	}
	res, err := s.bili.PollQRLogin(key)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if res.Code == 0 && len(res.Cookies) > 0 {
		uid := biliapi.UIDFromCookies(res.Cookies)
		uname, _ := s.bili.FetchUsername(res.CookieHeader)
		if uname == "" {
			uname = uid
		}
		acc := &biliapi.Account{
			UID:      uid,
			Username: uname,
			Cookies:  res.Cookies,
		}
		if err := s.accounts.SaveAccount(acc); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"code":     0,
			"message":  "ok",
			"uid":      uid,
			"username": uname,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"code":    res.Code,
		"message": res.Message,
	})
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":   s.accounts.ActiveUID(),
		"accounts": s.accounts.List(),
	})
}

func (s *Server) handleAccountSub(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/auth/accounts/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	uid := parts[0]
	if len(parts) == 2 && parts[1] == "activate" && r.Method == http.MethodPost {
		if err := s.accounts.SetActive(uid); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if err := s.accounts.Delete(uid); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (s *Server) activeCookieHeader() (string, error) {
	acc, err := s.accounts.Active()
	if err != nil {
		return "", err
	}
	return biliapi.CookieHeaderFromAccount(acc), nil
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/project/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project id required"})
		return
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid project id"})
		return
	}
	cookie, err := s.activeCookieHeader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请先登录账号"})
		return
	}
	if len(parts) == 1 || parts[1] == "" || parts[1] == "detail" {
		data, err := s.bili.GetProject(pid, cookie)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		name, _ := data["name"].(string)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"id":      pid,
			"name":    name,
			"options": biliapi.BuildTicketOptions(data),
			"raw":     data,
		})
		return
	}
	switch parts[1] {
	case "buyers":
		list, err := s.bili.GetBuyers(cookie)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	case "addresses":
		list, err := s.bili.GetAddresses(cookie)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	case "context":
		data, err := s.bili.GetProject(pid, cookie)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		buyers, _ := s.bili.GetBuyers(cookie)
		addrs, _ := s.bili.GetAddresses(cookie)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"project":        data,
			"ticket_options": biliapi.BuildTicketOptions(data),
			"buyers":         buyers,
			"addresses":      addrs,
		})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (s *Server) handleGenerateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 对齐 Buy on_submit_all：选择索引服务端生成，JSON 形态与 Buy 样例一致
	var body struct {
		Name         string `json:"name"`
		ProjectID    int    `json:"project_id"`
		TicketIndex  *int   `json:"ticket_index"`
		BuyerIndices []int  `json:"buyer_indices"`
		AddressIndex *int   `json:"address_index"`
		Buyer        string `json:"buyer"`
		Tel          string `json:"tel"`
		Phone        string `json:"phone"`
		StartTask    bool   `json:"start_task"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	acc, err := s.accounts.Active()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请先登录账号"})
		return
	}
	cookies := make([]ticketcfg.Cookie, 0, len(acc.Cookies))
	for _, c := range acc.Cookies {
		cookies = append(cookies, ticketcfg.Cookie{
			Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
			Expires: c.Expires, HttpOnly: c.HttpOnly, Secure: c.Secure, SameSite: c.SameSite,
		})
	}

	if body.TicketIndex == nil || body.ProjectID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请使用 ticket_index / buyer_indices / address_index 生成（与 Buy 一致）"})
		return
	}
	cookieHdr, err := s.activeCookieHeader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请先登录账号"})
		return
	}
	data, err := s.bili.GetProject(body.ProjectID, cookieHdr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	opts := biliapi.BuildTicketOptions(data)
	if *body.TicketIndex < 0 || *body.TicketIndex >= len(opts) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效票档索引"})
		return
	}
	buyers, err := s.bili.GetBuyers(cookieHdr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	addrs, err := s.bili.GetAddresses(cookieHdr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(body.BuyerIndices) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请至少选择一位购票人"})
		return
	}
	selectedBuyers := make([]map[string]interface{}, 0, len(body.BuyerIndices))
	for _, idx := range body.BuyerIndices {
		if idx < 0 || idx >= len(buyers) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效购票人索引"})
			return
		}
		selectedBuyers = append(selectedBuyers, buyers[idx])
	}
	if body.AddressIndex == nil || *body.AddressIndex < 0 || *body.AddressIndex >= len(addrs) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请选择收货地址"})
		return
	}
	projectName, _ := data["name"].(string)
	cfg, fileBase, err := ticketcfg.BuildFromSelection(ticketcfg.SelectionInput{
		Username:     acc.Username,
		ProjectName:  projectName,
		ProjectID:    body.ProjectID,
		Phone:        body.Phone,
		Ticket:       opts[*body.TicketIndex],
		Buyers:       selectedBuyers,
		Address:      addrs[*body.AddressIndex],
		BuyerName:    body.Buyer,
		BuyerTel:     body.Tel,
		Cookies:      cookies,
		FileNameHint: body.Name,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	raw, err := ticketcfg.MarshalConfigFile(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// 直接写盘，避免 map 重排字段
	task, err := s.master.AddTaskFromJSONBytes(fileBase, raw, true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         task.ID,
		"name":       task.TaskName,
		"path":       task.TaskName + ".json",
		"detail":     cfg.Detail,
		"sale_start": cfg.SaleStart,
		"start_task": body.StartTask,
	})
}

func ListenAndServe(addr string, m *master.Server, token string, configPath string) error {
	srv := New(m, token, configPath)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Printf("[web] listening on %s\n", addr)
	return httpSrv.ListenAndServe()
}
