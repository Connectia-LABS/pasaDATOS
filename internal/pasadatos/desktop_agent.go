package pasadatos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type DesktopSettingsUpdate struct {
	DeviceName      *string `json:"device_name,omitempty"`
	ReceiveFolder   *string `json:"receive_folder,omitempty"`
	RemoteServerURL *string `json:"remote_server_url,omitempty"`
	ActiveMode      *string `json:"active_mode,omitempty"`
	AutoStart       *bool   `json:"auto_start,omitempty"`
	AutoDownload    *bool   `json:"auto_download,omitempty"`
}

type DesktopAgent struct {
	mu         sync.RWMutex
	config     DesktopConfig
	configPath string
	dataDir    string
	store      *Store
	history    *HistoryStore
	jobs       *JobManager
	logger     Logger
	selected   []SelectedFile

	remote        *RemoteClient
	remoteOnline  bool
	remoteError   string
	remotePeers   []PeerView
	remotePairing *Pairing
	remoteJoinURL string

	inflightMu sync.Mutex
	inflight   map[string]struct{}

	cancel context.CancelFunc
}

func NewDesktopAgent(dataDir, configPath string, store *Store, logger Logger) (*DesktopAgent, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "[pasaDATOS] ", log.LstdFlags|log.LUTC)
	}
	cfg, err := LoadDesktopConfig(configPath)
	if err != nil {
		return nil, err
	}
	if err := ensureDir(cfg.ReceiveFolder); err != nil {
		return nil, fmt.Errorf("no se pudo preparar la carpeta de recepción: %w", err)
	}
	history, err := NewHistoryStore(filepath.Join(dataDir, "history.json"))
	if err != nil {
		return nil, err
	}
	a := &DesktopAgent{
		config: cfg, configPath: configPath, dataDir: dataDir, store: store,
		history: history, jobs: NewJobManager(), logger: logger,
		selected: []SelectedFile{}, remotePeers: []PeerView{}, inflight: make(map[string]struct{}),
	}
	if _, err := store.RegisterDevice(cfg.DeviceID, cfg.DeviceToken, cfg.DeviceName, "windows"); err != nil {
		return nil, fmt.Errorf("no se pudo registrar la PC local: %w", err)
	}
	a.rebuildRemoteLocked()
	return a, nil
}

func (a *DesktopAgent) NativeToken() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config.NativeToken
}

func (a *DesktopAgent) Config() DesktopConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

func (a *DesktopAgent) Start(parent context.Context) {
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel
	a.mu.Unlock()
	go a.loop(ctx)
}

func (a *DesktopAgent) Stop() {
	a.mu.Lock()
	cancel := a.cancel
	a.cancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *DesktopAgent) loop(ctx context.Context) {
	ticker := time.NewTicker(2500 * time.Millisecond)
	pruneTicker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	defer pruneTicker.Stop()
	a.syncAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.syncAll(ctx)
		case <-pruneTicker.C:
			a.jobs.Prune()
		}
	}
}

func (a *DesktopAgent) syncAll(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	a.syncLocal(ctx)
	a.syncRemote(ctx)
}

func (a *DesktopAgent) syncLocal(ctx context.Context) {
	cfg := a.Config()
	_, _ = a.store.RegisterDevice(cfg.DeviceID, cfg.DeviceToken, cfg.DeviceName, "windows")
	if cfg.AutoDownload {
		for _, transfer := range a.store.ListTransfers(cfg.DeviceID, "inbox", StatusReady, 100) {
			go a.ReceiveLocalTransfer(transfer.ID)
		}
	}
	for _, transfer := range a.store.ListTransfers(cfg.DeviceID, "outbox", "", 200) {
		a.reflectOutgoingStatus(transfer, "local")
	}
	_ = ctx
}

func (a *DesktopAgent) syncRemote(ctx context.Context) {
	a.mu.RLock()
	client := a.remote
	configured := strings.TrimSpace(a.config.RemoteServerURL) != ""
	a.mu.RUnlock()
	if !configured || client == nil {
		a.mu.Lock()
		a.remoteOnline = false
		a.remoteError = ""
		a.remotePeers = []PeerView{}
		a.mu.Unlock()
		return
	}
	if _, err := client.Register(ctx); err != nil {
		a.setRemoteFailure(err)
		return
	}
	peers, err := client.ListPeers(ctx)
	if err != nil {
		a.setRemoteFailure(err)
		return
	}
	a.mu.Lock()
	a.remoteOnline = true
	a.remoteError = ""
	a.remotePeers = peers
	if a.remotePairing != nil && !a.remotePairing.ExpiresAt.After(time.Now().UTC()) {
		a.remotePairing = nil
		a.remoteJoinURL = ""
	}
	a.mu.Unlock()
	inbox, err := client.ListTransfers(ctx, "inbox", StatusReady, 100)
	if err == nil && a.Config().AutoDownload {
		for _, transfer := range inbox {
			go a.ReceiveRemoteTransfer(transfer.ID)
		}
	}
	outbox, err := client.ListTransfers(ctx, "outbox", "", 200)
	if err == nil {
		for _, transfer := range outbox {
			a.reflectOutgoingStatus(transfer, "remote")
		}
	}
}

func (a *DesktopAgent) setRemoteFailure(err error) {
	a.mu.Lock()
	a.remoteOnline = false
	a.remoteError = userFacingError(err)
	a.mu.Unlock()
}

func userFacingError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "Error desconocido"
	}
	return message
}

func (a *DesktopAgent) rebuildRemoteLocked() {
	if strings.TrimSpace(a.config.RemoteServerURL) == "" {
		a.remote = nil
		return
	}
	a.remote = NewRemoteClient(a.config.RemoteServerURL, a.config.DeviceID, a.config.DeviceToken, a.config.DeviceName, "windows")
}

func listenPort(address string) string {
	_, port, err := net.SplitHostPort(address)
	if err == nil && port != "" {
		return port
	}
	parts := strings.Split(address, ":")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return "8765"
}

func (a *DesktopAgent) localURLs() (serverURL, displayURL string) {
	cfg := a.Config()
	port := listenPort(cfg.ListenAddress)
	serverURL = "http://127.0.0.1:" + port
	ips := localIPv4s()
	if len(ips) > 0 {
		displayURL = "http://" + ips[0] + ":" + port + "/"
	} else {
		displayURL = serverURL + "/"
	}
	return serverURL, displayURL
}

func (a *DesktopAgent) BuildState() DesktopState {
	cfg := a.Config()
	serverURL, displayURL := a.localURLs()
	localPairing := a.store.CurrentPairing(cfg.DeviceID)
	local := ModeState{
		Mode: "local", Configured: true, Online: true, ServerURL: serverURL,
		DisplayURL: displayURL, Peers: a.store.ListPeers(cfg.DeviceID),
	}
	if localPairing != nil {
		local.PairingCode = localPairing.Code
		local.PairingExpiry = &localPairing.ExpiresAt
		local.JoinURL = strings.TrimRight(displayURL, "/") + "/?pair=" + localPairing.Code
	}
	a.mu.RLock()
	remote := ModeState{
		Mode: "remote", Configured: strings.TrimSpace(cfg.RemoteServerURL) != "",
		Online: a.remoteOnline, ServerURL: cfg.RemoteServerURL, DisplayURL: cfg.RemoteServerURL,
		Peers: append([]PeerView(nil), a.remotePeers...), Error: a.remoteError,
	}
	if a.remotePairing != nil && a.remotePairing.ExpiresAt.After(time.Now().UTC()) {
		pairing := *a.remotePairing
		remote.PairingCode = pairing.Code
		remote.PairingExpiry = &pairing.ExpiresAt
		remote.JoinURL = a.remoteJoinURL
	}
	selected := append([]SelectedFile(nil), a.selected...)
	a.mu.RUnlock()
	return DesktopState{
		AppName: AppName, Version: AppVersion, DeviceID: cfg.DeviceID, DeviceName: cfg.DeviceName,
		ReceiveFolder: cfg.ReceiveFolder, ActiveMode: cfg.ActiveMode, AutoStart: cfg.AutoStart,
		AutoDownload: cfg.AutoDownload, Local: local, Remote: remote,
		Jobs: a.jobs.List(), History: a.history.List(250), SelectedFiles: selected, Now: time.Now().UTC(),
	}
}

func (a *DesktopAgent) SelectFiles() ([]SelectedFile, error) {
	paths, err := nativePickFiles()
	if err != nil {
		return nil, err
	}
	selected := inspectSelectedFiles(paths)
	a.mu.Lock()
	a.selected = selected
	a.mu.Unlock()
	return selected, nil
}

func inspectSelectedFiles(paths []string) []SelectedFile {
	out := make([]SelectedFile, 0, len(paths))
	seen := map[string]struct{}{}
	for _, raw := range paths {
		path := filepath.Clean(strings.TrimSpace(raw))
		if path == "." || path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, SelectedFile{Path: path, Name: info.Name(), Size: info.Size()})
	}
	return out
}

func (a *DesktopAgent) SelectReceiveFolder() (string, error) {
	folder, err := nativePickFolder()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(folder) == "" {
		return "", errors.New("no se seleccionó ninguna carpeta")
	}
	value := folder
	if _, err := a.UpdateSettings(DesktopSettingsUpdate{ReceiveFolder: &value}); err != nil {
		return "", err
	}
	return folder, nil
}

func (a *DesktopAgent) SetSelected(paths []string) []SelectedFile {
	selected := inspectSelectedFiles(paths)
	a.mu.Lock()
	a.selected = selected
	a.mu.Unlock()
	return selected
}

func (a *DesktopAgent) StageFile(filename string, size int64, source io.Reader) (SelectedFile, error) {
	filename = sanitizeFilename(filename)
	if size < 0 {
		return SelectedFile{}, errors.New("tamaño de archivo inválido")
	}
	stageDir := filepath.Join(a.dataDir, "staging")
	if err := ensureDir(stageDir); err != nil {
		return SelectedFile{}, err
	}
	path := collisionSafePath(stageDir, filename)
	written, _, err := copyFileWithProgress(path, source, size, nil)
	if err != nil {
		return SelectedFile{}, err
	}
	if size > 0 && written != size {
		_ = os.Remove(path)
		return SelectedFile{}, fmt.Errorf("archivo incompleto: %d de %d bytes", written, size)
	}
	selected := SelectedFile{Path: path, Name: filename, Size: written}
	a.mu.Lock()
	a.selected = append(a.selected, selected)
	a.mu.Unlock()
	return selected, nil
}

func (a *DesktopAgent) ClearSelected() {
	a.mu.Lock()
	a.selected = []SelectedFile{}
	a.mu.Unlock()
}

func (a *DesktopAgent) CreatePairing(ctx context.Context, mode string) (Pairing, string, error) {
	cfg := a.Config()
	if mode == "remote" {
		a.mu.RLock()
		client := a.remote
		a.mu.RUnlock()
		if client == nil {
			return Pairing{}, "", errors.New("configurá primero el servidor remoto")
		}
		if _, err := client.Register(ctx); err != nil {
			return Pairing{}, "", err
		}
		pairing, joinURL, err := client.CreatePairing(ctx)
		if err == nil {
			a.mu.Lock()
			a.remotePairing = &pairing
			a.remoteJoinURL = joinURL
			a.mu.Unlock()
		}
		return pairing, joinURL, err
	}
	pairing, err := a.store.CreatePairing(cfg.DeviceID)
	if err != nil {
		return Pairing{}, "", err
	}
	_, displayURL := a.localURLs()
	return pairing, strings.TrimRight(displayURL, "/") + "/?pair=" + pairing.Code, nil
}

func (a *DesktopAgent) JoinPairing(ctx context.Context, mode, code string) (PeerView, error) {
	cfg := a.Config()
	if mode == "remote" {
		a.mu.RLock()
		client := a.remote
		a.mu.RUnlock()
		if client == nil {
			return PeerView{}, errors.New("configurá primero el servidor remoto")
		}
		if _, err := client.Register(ctx); err != nil {
			return PeerView{}, err
		}
		peer, err := client.JoinPairing(ctx, code)
		if err == nil {
			go a.syncAll(context.Background())
		}
		return peer, err
	}
	return a.store.JoinPairing(cfg.DeviceID, code)
}

func (a *DesktopAgent) Unlink(ctx context.Context, mode, peerID string) error {
	cfg := a.Config()
	if mode == "remote" {
		a.mu.RLock()
		client := a.remote
		a.mu.RUnlock()
		if client == nil {
			return errors.New("servidor remoto no configurado")
		}
		if err := client.Unlink(ctx, peerID); err != nil {
			return err
		}
		go a.syncAll(context.Background())
		return nil
	}
	return a.store.Unlink(cfg.DeviceID, peerID)
}

func (a *DesktopAgent) SendFiles(_ context.Context, mode, peerID string, paths []string) ([]TransferJob, error) {
	if mode != "remote" {
		mode = "local"
	}
	selected := inspectSelectedFiles(paths)
	if len(selected) == 0 {
		return nil, errors.New("seleccioná al menos un archivo válido")
	}
	if strings.TrimSpace(peerID) == "" {
		return nil, errors.New("seleccioná un dispositivo de destino")
	}
	jobs := make([]TransferJob, 0, len(selected))
	for _, file := range selected {
		job := a.jobs.Add(TransferJob{
			Direction: "sent", Mode: mode, PeerID: peerID, Filename: file.Name,
			Size: file.Size, Status: StatusPending, SourcePath: file.Path,
		})
		jobs = append(jobs, job)
		if mode == "remote" {
			go a.sendRemoteFile(job, file, peerID)
		} else {
			go a.sendLocalFile(job, file, peerID)
		}
	}
	a.ClearSelected()
	return jobs, nil
}

func (a *DesktopAgent) sendLocalFile(job TransferJob, file SelectedFile, peerID string) {
	cfg := a.Config()
	contentType := mime.TypeByExtension(filepath.Ext(file.Name))
	transfer, err := a.store.CreateTransfer(cfg.DeviceID, peerID, file.Name, contentType, file.Size, file.Path, "linked", file.Path)
	if err != nil {
		a.failJob(job, err)
		return
	}
	peerName := transfer.ReceiverName
	now := time.Now().UTC()
	a.jobs.Update(job.ID, func(item *TransferJob) {
		item.TransferID = transfer.ID
		item.PeerName = peerName
		item.Done = file.Size
		item.Status = StatusReady
		item.CompletedAt = &now
	})
	_ = a.history.Add(HistoryItem{
		TransferID: transfer.ID, Direction: "sent", Mode: "local", PeerID: peerID,
		PeerName: peerName, Filename: file.Name, Size: file.Size, Status: StatusReady,
		Progress: file.Size, SourcePath: file.Path, StartedAt: job.StartedAt, CompletedAt: &now,
	})
}

func (a *DesktopAgent) sendRemoteFile(job TransferJob, file SelectedFile, peerID string) {
	a.mu.RLock()
	client := a.remote
	a.mu.RUnlock()
	if client == nil {
		a.failJob(job, errors.New("servidor remoto no configurado"))
		return
	}
	ctx := context.Background()
	contentType := mime.TypeByExtension(filepath.Ext(file.Name))
	transfer, err := client.CreateTransfer(ctx, peerID, file.Name, contentType, file.Size, file.Path)
	if err != nil {
		a.failJob(job, err)
		return
	}
	a.jobs.Update(job.ID, func(item *TransferJob) {
		item.TransferID = transfer.ID
		item.PeerName = transfer.ReceiverName
		item.Status = StatusUploading
	})
	transfer, err = client.UploadFile(ctx, transfer, file.Path, func(done int64) {
		a.jobs.Update(job.ID, func(item *TransferJob) { item.Done = done; item.Status = StatusUploading })
	})
	if err != nil {
		a.failJob(job, err)
		return
	}
	now := time.Now().UTC()
	a.jobs.Update(job.ID, func(item *TransferJob) {
		item.Done = file.Size
		item.Status = StatusReady
		item.PeerName = transfer.ReceiverName
		item.CompletedAt = &now
	})
	_ = a.history.Add(HistoryItem{
		TransferID: transfer.ID, Direction: "sent", Mode: "remote", PeerID: peerID,
		PeerName: transfer.ReceiverName, Filename: file.Name, Size: file.Size,
		Status: StatusReady, Progress: file.Size, SourcePath: file.Path,
		StartedAt: job.StartedAt, CompletedAt: &now,
	})
}

func (a *DesktopAgent) failJob(job TransferJob, err error) {
	now := time.Now().UTC()
	a.jobs.Update(job.ID, func(item *TransferJob) {
		item.Status = StatusError
		item.Error = userFacingError(err)
		item.CompletedAt = &now
	})
	_ = a.history.Add(HistoryItem{
		TransferID: job.TransferID, Direction: job.Direction, Mode: job.Mode,
		PeerID: job.PeerID, PeerName: job.PeerName, Filename: job.Filename, Size: job.Size,
		Status: StatusError, Progress: job.Done, SourcePath: job.SourcePath,
		DestinationPath: job.DestinationPath, StartedAt: job.StartedAt, CompletedAt: &now,
		Error: userFacingError(err),
	})
}

func (a *DesktopAgent) claim(mode, transferID string) bool {
	key := mode + ":" + transferID
	a.inflightMu.Lock()
	defer a.inflightMu.Unlock()
	if _, exists := a.inflight[key]; exists {
		return false
	}
	a.inflight[key] = struct{}{}
	return true
}

func (a *DesktopAgent) release(mode, transferID string) {
	a.inflightMu.Lock()
	delete(a.inflight, mode+":"+transferID)
	a.inflightMu.Unlock()
}

func (a *DesktopAgent) ReceiveLocalTransfer(transferID string) {
	if !a.claim("local", transferID) {
		return
	}
	defer a.release("local", transferID)
	cfg := a.Config()
	transfer, err := a.store.GetTransferForDevice(transferID, cfg.DeviceID)
	if err != nil || transfer.ReceiverDeviceID != cfg.DeviceID || transfer.Status != StatusReady {
		return
	}
	path, err := a.store.TransferFilePath(transfer)
	if err != nil {
		a.recordReceiveFailure(transfer, "local", err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		a.recordReceiveFailure(transfer, "local", err)
		return
	}
	defer file.Close()
	destination := collisionSafePath(cfg.ReceiveFolder, transfer.Filename)
	job := a.jobs.Add(TransferJob{
		TransferID: transfer.ID, Direction: "received", Mode: "local", PeerID: transfer.SenderDeviceID,
		PeerName: transfer.SenderDisplayName, Filename: transfer.Filename, Size: transfer.Size,
		Status: StatusUploading, SourcePath: transfer.SourceLabel, DestinationPath: destination,
	})
	_ = a.history.Add(HistoryItem{
		TransferID: transfer.ID, Direction: "received", Mode: "local", PeerID: transfer.SenderDeviceID,
		PeerName: transfer.SenderDisplayName, Filename: transfer.Filename, Size: transfer.Size,
		Status: StatusUploading, SourcePath: transfer.SourceLabel, DestinationPath: destination,
		StartedAt: job.StartedAt,
	})
	written, _, err := copyFileWithProgress(destination, file, transfer.Size, func(done int64) {
		a.jobs.Update(job.ID, func(item *TransferJob) { item.Done = done })
	})
	if err != nil {
		a.completeReceiveFailure(job, transfer, err)
		return
	}
	_, err = a.store.MarkDelivered(transfer.ID, cfg.DeviceID, destination)
	if err != nil {
		a.completeReceiveFailure(job, transfer, err)
		return
	}
	a.completeReceive(job, transfer, written, destination)
}

func (a *DesktopAgent) ReceiveRemoteTransfer(transferID string) {
	if !a.claim("remote", transferID) {
		return
	}
	defer a.release("remote", transferID)
	a.mu.RLock()
	client := a.remote
	a.mu.RUnlock()
	if client == nil {
		return
	}
	ctx := context.Background()
	transfer, err := client.GetTransfer(ctx, transferID)
	cfg := a.Config()
	if err != nil || transfer.ReceiverDeviceID != cfg.DeviceID || transfer.Status != StatusReady {
		return
	}
	destination := collisionSafePath(cfg.ReceiveFolder, transfer.Filename)
	job := a.jobs.Add(TransferJob{
		TransferID: transfer.ID, Direction: "received", Mode: "remote", PeerID: transfer.SenderDeviceID,
		PeerName: transfer.SenderDisplayName, Filename: transfer.Filename, Size: transfer.Size,
		Status: StatusUploading, SourcePath: transfer.SourceLabel, DestinationPath: destination,
	})
	_ = a.history.Add(HistoryItem{
		TransferID: transfer.ID, Direction: "received", Mode: "remote", PeerID: transfer.SenderDeviceID,
		PeerName: transfer.SenderDisplayName, Filename: transfer.Filename, Size: transfer.Size,
		Status: StatusUploading, SourcePath: transfer.SourceLabel, DestinationPath: destination,
		StartedAt: job.StartedAt,
	})
	written, _, err := client.DownloadTo(ctx, transfer, destination, func(done int64) {
		a.jobs.Update(job.ID, func(item *TransferJob) { item.Done = done })
	})
	if err != nil {
		a.completeReceiveFailure(job, transfer, err)
		return
	}
	if _, err := client.MarkReceived(ctx, transfer.ID, destination); err != nil {
		a.completeReceiveFailure(job, transfer, err)
		return
	}
	a.completeReceive(job, transfer, written, destination)
}

func (a *DesktopAgent) recordReceiveFailure(transfer Transfer, mode string, err error) {
	now := time.Now().UTC()
	_ = a.history.Add(HistoryItem{
		TransferID: transfer.ID, Direction: "received", Mode: mode,
		PeerID: transfer.SenderDeviceID, PeerName: transfer.SenderDisplayName,
		Filename: transfer.Filename, Size: transfer.Size, Status: StatusError,
		SourcePath: transfer.SourceLabel, StartedAt: now, CompletedAt: &now, Error: userFacingError(err),
	})
}

func (a *DesktopAgent) completeReceive(job TransferJob, transfer Transfer, written int64, destination string) {
	now := time.Now().UTC()
	a.jobs.Update(job.ID, func(item *TransferJob) {
		item.Done = written
		item.Status = StatusDelivered
		item.CompletedAt = &now
	})
	_ = a.history.UpdateByTransferID(transfer.ID, func(item *HistoryItem) {
		item.Progress = written
		item.Status = StatusDelivered
		item.DestinationPath = destination
		item.CompletedAt = &now
		item.Error = ""
	})
}

func (a *DesktopAgent) completeReceiveFailure(job TransferJob, transfer Transfer, err error) {
	now := time.Now().UTC()
	a.jobs.Update(job.ID, func(item *TransferJob) {
		item.Status = StatusError
		item.Error = userFacingError(err)
		item.CompletedAt = &now
	})
	_ = a.history.UpdateByTransferID(transfer.ID, func(item *HistoryItem) {
		item.Status = StatusError
		item.Error = userFacingError(err)
		item.CompletedAt = &now
	})
}

func (a *DesktopAgent) reflectOutgoingStatus(transfer Transfer, mode string) {
	if transfer.SenderDeviceID != a.Config().DeviceID {
		return
	}
	item, ok := a.history.FindByTransferID(transfer.ID)
	if !ok {
		return
	}
	if item.Status == transfer.Status && item.Progress == transfer.UploadedBytes {
		return
	}
	_ = a.history.UpdateByTransferID(transfer.ID, func(history *HistoryItem) {
		history.Status = transfer.Status
		history.Progress = transfer.UploadedBytes
		if transfer.Status == StatusDelivered {
			history.DestinationPath = transfer.DestinationLabel
			if transfer.DeliveredAt != nil {
				history.CompletedAt = transfer.DeliveredAt
			}
		}
		if transfer.Status == StatusError || transfer.Status == StatusCancelled {
			history.Error = transfer.Error
		}
	})
	a.jobs.UpdateByTransferID(transfer.ID, func(job *TransferJob) {
		job.Status = transfer.Status
		job.Done = transfer.UploadedBytes
		if transfer.Status == StatusDelivered && transfer.DeliveredAt != nil {
			job.CompletedAt = transfer.DeliveredAt
		}
	})
	_ = mode
}

func (a *DesktopAgent) UpdateSettings(update DesktopSettingsUpdate) (DesktopConfig, error) {
	a.mu.Lock()
	cfg := a.config
	oldAutoStart := cfg.AutoStart
	if update.DeviceName != nil {
		name := strings.TrimSpace(*update.DeviceName)
		if name == "" {
			a.mu.Unlock()
			return DesktopConfig{}, errors.New("el nombre del dispositivo no puede quedar vacío")
		}
		if len([]rune(name)) > 80 {
			name = string([]rune(name)[:80])
		}
		cfg.DeviceName = name
	}
	if update.ReceiveFolder != nil {
		folder := filepath.Clean(strings.TrimSpace(*update.ReceiveFolder))
		if folder == "." || folder == "" {
			a.mu.Unlock()
			return DesktopConfig{}, errors.New("carpeta de recepción inválida")
		}
		if err := ensureDir(folder); err != nil {
			a.mu.Unlock()
			return DesktopConfig{}, err
		}
		cfg.ReceiveFolder = folder
	}
	if update.RemoteServerURL != nil {
		normalized, err := normalizeServerURL(*update.RemoteServerURL)
		if err != nil {
			a.mu.Unlock()
			return DesktopConfig{}, err
		}
		cfg.RemoteServerURL = normalized
	}
	if update.ActiveMode != nil {
		if *update.ActiveMode == "remote" {
			cfg.ActiveMode = "remote"
		} else {
			cfg.ActiveMode = "local"
		}
	}
	if update.AutoStart != nil {
		cfg.AutoStart = *update.AutoStart
	}
	if update.AutoDownload != nil {
		cfg.AutoDownload = *update.AutoDownload
	}
	cfg.UpdatedAt = time.Now().UTC()
	if err := SaveDesktopConfig(a.configPath, cfg); err != nil {
		a.mu.Unlock()
		return DesktopConfig{}, err
	}
	a.config = cfg
	a.rebuildRemoteLocked()
	a.mu.Unlock()
	if oldAutoStart != cfg.AutoStart {
		if err := nativeSetAutoStart(cfg.AutoStart); err != nil {
			a.logger.Printf("no se pudo actualizar inicio automático: %v", err)
		}
	}
	_, _ = a.store.RegisterDevice(cfg.DeviceID, cfg.DeviceToken, cfg.DeviceName, "windows")
	go a.syncAll(context.Background())
	return cfg, nil
}

func (a *DesktopAgent) OpenPath(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return errors.New("ruta inválida")
	}
	return nativeOpenPath(path)
}

func (a *DesktopAgent) OpenReceiveFolder() error {
	return nativeOpenPath(a.Config().ReceiveFolder)
}

func (a *DesktopAgent) ClearHistory() error {
	return a.history.Clear()
}

func (a *DesktopAgent) SortedPeers(mode string) []PeerView {
	state := a.BuildState()
	peers := state.Local.Peers
	if mode == "remote" {
		peers = state.Remote.Peers
	}
	sort.SliceStable(peers, func(i, j int) bool {
		if peers[i].Online != peers[j].Online {
			return peers[i].Online
		}
		return strings.ToLower(peers[i].Name) < strings.ToLower(peers[j].Name)
	})
	return peers
}
