package master

import (
	"biliTickerStorm/internal/common/workercfg"
	"sync"
)

// workerSettings 内存缓存，与 CONFIG_PATH/worker_settings.json 同步。
type workerSettingsStore struct {
	mu      sync.RWMutex
	cfg     workercfg.Settings
	version int64
	path    string
}

func newWorkerSettingsStore(configPath string) *workerSettingsStore {
	st := &workerSettingsStore{path: configPath}
	s, ver, err := workercfg.Load(configPath)
	if err != nil {
		log.Warnf("加载 worker_settings.json 失败: %v", err)
		return st
	}
	st.cfg = s
	st.version = ver
	if ver > 0 {
		log.Infof("已加载 worker_settings.json version=%d", ver)
	}
	return st
}

func (st *workerSettingsStore) Get() (workercfg.Settings, int64) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.cfg, st.version
}

func (st *workerSettingsStore) GetMasked() (workercfg.Settings, int64) {
	s, v := st.Get()
	return s.Masked(), v
}

func (st *workerSettingsStore) Save(s workercfg.Settings) (int64, error) {
	s.Normalize()
	ver, err := workercfg.Save(st.path, s)
	if err != nil {
		return 0, err
	}
	st.mu.Lock()
	st.cfg = s
	st.version = ver
	st.mu.Unlock()
	return ver, nil
}

func (st *workerSettingsStore) ConfigJSON() (string, int64) {
	s, ver := st.Get()
	if ver == 0 {
		// 空配置也下发 version=0，worker 可不覆盖
		b, _ := s.ToJSON()
		return string(b), 0
	}
	b, err := s.ToJSON()
	if err != nil {
		return "", ver
	}
	return string(b), ver
}
