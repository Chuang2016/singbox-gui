package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store persists nodes/subs/settings as JSON.
type Store struct {
	mu   sync.Mutex
	path string
	Data StoreData
}

func configDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "sbgui")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".sbgui")
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".writetest")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// WorkDir returns the working dir: the folder containing the sbgui executable
// (portable: sing-box binary sits next to sbgui). Falls back to the user
// config dir when the exe folder is not writable (e.g. Program Files).
func WorkDir() string {
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" && writable(dir) {
			return dir
		}
	}
	return configDir()
}

func OpenStore(workDir string) (*Store, error) {
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return nil, err
	}
	// migrate from the pre-v1.0.3 location (%APPDATA%/sbgui) if needed
	newPath := filepath.Join(workDir, "store.json")
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		if oldRaw, err := os.ReadFile(filepath.Join(configDir(), "store.json")); err == nil {
			_ = os.WriteFile(newPath, oldRaw, 0600)
		}
	}
	s := &Store{path: newPath}
	s.Data.Cfg = DefaultSettings()
	if raw, err := os.ReadFile(s.path); err == nil {
		var d StoreData
		if err := json.Unmarshal(raw, &d); err == nil {
			if d.Cfg.MixedPort == 0 {
				d.Cfg = DefaultSettings()
			}
			s.Data = d
		}
	}
	return s, nil
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

var idSeq int64

func NewID(prefix string) string {
	idSeq++
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), idSeq)
}
