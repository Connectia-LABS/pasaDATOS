package pasadatos

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type HistoryStore struct {
	mu   sync.RWMutex
	path string
	data HistoryData
}

func NewHistoryStore(path string) (*HistoryStore, error) {
	h := &HistoryStore{path: path, data: HistoryData{Version: 1, Items: []HistoryItem{}}}
	if err := readJSON(path, &h.data); err != nil && !os.IsNotExist(err) {
		corrupt := path + ".corrupt-" + time.Now().Format("20060102-150405")
		_ = os.Rename(path, corrupt)
		h.data = HistoryData{Version: 1, Items: []HistoryItem{}}
	}
	if h.data.Version == 0 {
		h.data.Version = 1
	}
	if h.data.Items == nil {
		h.data.Items = []HistoryItem{}
	}
	return h, h.persistLocked()
}

func (h *HistoryStore) persistLocked() error {
	return writeJSONAtomic(h.path, &h.data)
}

func (h *HistoryStore) Add(item HistoryItem) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if item.ID == "" {
		item.ID = randomID("hist")
	}
	if item.StartedAt.IsZero() {
		item.StartedAt = time.Now().UTC()
	}
	h.data.Items = append([]HistoryItem{item}, h.data.Items...)
	if len(h.data.Items) > 2000 {
		h.data.Items = h.data.Items[:2000]
	}
	return h.persistLocked()
}

func (h *HistoryStore) UpdateByID(id string, mutate func(*HistoryItem)) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.data.Items {
		if h.data.Items[i].ID == id {
			mutate(&h.data.Items[i])
			return h.persistLocked()
		}
	}
	return ErrNotFound
}

func (h *HistoryStore) UpdateByTransferID(transferID string, mutate func(*HistoryItem)) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.data.Items {
		if h.data.Items[i].TransferID == transferID {
			mutate(&h.data.Items[i])
			return h.persistLocked()
		}
	}
	return ErrNotFound
}

func (h *HistoryStore) FindByTransferID(transferID string) (HistoryItem, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, item := range h.data.Items {
		if item.TransferID == transferID {
			return item, true
		}
	}
	return HistoryItem{}, false
}

func (h *HistoryStore) List(limit int) []HistoryItem {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if limit <= 0 || limit > len(h.data.Items) {
		limit = len(h.data.Items)
	}
	out := make([]HistoryItem, limit)
	copy(out, h.data.Items[:limit])
	return out
}

func (h *HistoryStore) Clear() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.data.Items = []HistoryItem{}
	return h.persistLocked()
}

type JobManager struct {
	mu   sync.RWMutex
	jobs map[string]TransferJob
}

func NewJobManager() *JobManager {
	return &JobManager{jobs: make(map[string]TransferJob)}
}

func (m *JobManager) Add(job TransferJob) TransferJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job.ID == "" {
		job.ID = randomID("job")
	}
	if job.StartedAt.IsZero() {
		job.StartedAt = time.Now().UTC()
	}
	m.jobs[job.ID] = job
	return job
}

func (m *JobManager) Update(id string, mutate func(*TransferJob)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return
	}
	mutate(&job)
	m.jobs[id] = job
}

func (m *JobManager) UpdateByTransferID(transferID string, mutate func(*TransferJob)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, job := range m.jobs {
		if job.TransferID == transferID {
			mutate(&job)
			m.jobs[id] = job
		}
	}
}

func (m *JobManager) List() []TransferJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TransferJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		out = append(out, job)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (m *JobManager) Prune() {
	cutoff := time.Now().UTC().Add(-10 * time.Minute)
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, job := range m.jobs {
		if job.CompletedAt != nil && job.CompletedAt.Before(cutoff) {
			delete(m.jobs, id)
		}
	}
}

func LoadDesktopConfig(path string) (DesktopConfig, error) {
	now := time.Now().UTC()
	cfg := DesktopConfig{
		Version:          1,
		DeviceID:         randomID("device"),
		DeviceToken:      randomToken(32),
		DeviceName:       defaultDeviceName(),
		NativeToken:      randomToken(32),
		ReceiveFolder:    defaultReceiveFolder(),
		RemoteServerURL:  "",
		ActiveMode:       "local",
		ListenAddress:    "0.0.0.0:8765",
		AutoStart:        true,
		AutoDownload:     true,
		LocalFileTTLHour: 24,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := readJSON(path, &cfg); err != nil {
		if !os.IsNotExist(err) {
			return DesktopConfig{}, err
		}
	}
	if cfg.DeviceID == "" {
		cfg.DeviceID = randomID("device")
	}
	if cfg.DeviceToken == "" {
		cfg.DeviceToken = randomToken(32)
	}
	if cfg.NativeToken == "" {
		cfg.NativeToken = randomToken(32)
	}
	if strings.TrimSpace(cfg.DeviceName) == "" {
		cfg.DeviceName = defaultDeviceName()
	}
	if strings.TrimSpace(cfg.ReceiveFolder) == "" {
		cfg.ReceiveFolder = defaultReceiveFolder()
	}
	if cfg.ActiveMode != "remote" {
		cfg.ActiveMode = "local"
	}
	if strings.TrimSpace(cfg.ListenAddress) == "" {
		cfg.ListenAddress = "0.0.0.0:8765"
	}
	if cfg.LocalFileTTLHour <= 0 {
		cfg.LocalFileTTLHour = 24
	}
	cfg.Version = 1
	cfg.UpdatedAt = now
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = now
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return DesktopConfig{}, err
	}
	if err := writeJSONAtomic(path, &cfg); err != nil {
		return DesktopConfig{}, err
	}
	return cfg, nil
}

func SaveDesktopConfig(path string, cfg DesktopConfig) error {
	if cfg.DeviceID == "" || cfg.DeviceToken == "" || cfg.NativeToken == "" {
		return errors.New("configuración de identidad incompleta")
	}
	cfg.UpdatedAt = time.Now().UTC()
	return writeJSONAtomic(path, &cfg)
}
