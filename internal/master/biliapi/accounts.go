package biliapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Account struct {
	UID      string   `json:"uid"`
	Username string   `json:"username"`
	Cookies  []Cookie `json:"cookies"`
	SavedAt  time.Time `json:"saved_at"`
}

type AccountStore struct {
	mu       sync.RWMutex
	dir      string
	active   string
	accounts map[string]*Account
}

func NewAccountStore(dir string) *AccountStore {
	s := &AccountStore{
		dir:      dir,
		accounts: make(map[string]*Account),
	}
	_ = os.MkdirAll(dir, 0o700)
	s.loadAll()
	return s
}

func (s *AccountStore) loadAll() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var acc Account
		if json.Unmarshal(b, &acc) != nil || acc.UID == "" {
			continue
		}
		s.accounts[acc.UID] = &acc
		if s.active == "" {
			s.active = acc.UID
		}
	}
	// active 标记
	if b, err := os.ReadFile(filepath.Join(s.dir, ".active")); err == nil {
		uid := string(b)
		if _, ok := s.accounts[uid]; ok {
			s.active = uid
		}
	}
}

func (s *AccountStore) save(acc *Account) error {
	b, err := json.MarshalIndent(acc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, acc.UID+".json"), b, 0o600)
}

func (s *AccountStore) SaveAccount(acc *Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc.SavedAt = time.Now()
	s.accounts[acc.UID] = acc
	if s.active == "" {
		s.active = acc.UID
		_ = os.WriteFile(filepath.Join(s.dir, ".active"), []byte(acc.UID), 0o600)
	}
	return s.save(acc)
}

func (s *AccountStore) List() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		cp := *a
		cp.Cookies = nil // 列表不返回 cookies
		out = append(out, cp)
	}
	return out
}

func (s *AccountStore) Get(uid string) (*Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[uid]
	if !ok {
		return nil, fmt.Errorf("account not found")
	}
	cp := *a
	return &cp, nil
}

func (s *AccountStore) Active() (*Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.active == "" {
		return nil, fmt.Errorf("no active account")
	}
	a, ok := s.accounts[s.active]
	if !ok {
		return nil, fmt.Errorf("no active account")
	}
	cp := *a
	return &cp, nil
}

func (s *AccountStore) ActiveUID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *AccountStore) SetActive(uid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[uid]; !ok {
		return fmt.Errorf("account not found")
	}
	s.active = uid
	return os.WriteFile(filepath.Join(s.dir, ".active"), []byte(uid), 0o600)
}

func (s *AccountStore) Delete(uid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.accounts, uid)
	_ = os.Remove(filepath.Join(s.dir, uid+".json"))
	if s.active == uid {
		s.active = ""
		for id := range s.accounts {
			s.active = id
			break
		}
		if s.active != "" {
			_ = os.WriteFile(filepath.Join(s.dir, ".active"), []byte(s.active), 0o600)
		} else {
			_ = os.Remove(filepath.Join(s.dir, ".active"))
		}
	}
	return nil
}

func CookieHeaderFromAccount(acc *Account) string {
	return CookiesToHeader(acc.Cookies)
}

func UIDFromCookies(cookies []Cookie) string {
	for _, c := range cookies {
		if c.Name == "DedeUserID" {
			return c.Value
		}
	}
	return fmt.Sprintf("acc-%d", time.Now().Unix())
}
