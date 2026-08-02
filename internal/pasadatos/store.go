package pasadatos

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnauthorized = errors.New("no autorizado")
	ErrNotFound     = errors.New("no encontrado")
	ErrNotLinked    = errors.New("los dispositivos no están vinculados")
	ErrExpired      = errors.New("el código o token venció")
	ErrConflict     = errors.New("conflicto")
)

type Store struct {
	mu                    sync.RWMutex
	state                 PersistentState
	statePath             string
	filesDir              string
	fileTTL               time.Duration
	metadataTTL           time.Duration
	pairingTTL            time.Duration
	downloadTokenTTL      time.Duration
	deliveredDeleteDelay  time.Duration
	maxFileBytes          int64
	logger                Logger
	lastProgressPersist   map[string]time.Time
	lastDownloadedPersist map[string]time.Time
}

func NewStore(options ServerOptions) (*Store, error) {
	if options.FileTTL <= 0 {
		options.FileTTL = 24 * time.Hour
	}
	if options.MetadataTTL <= 0 {
		options.MetadataTTL = 30 * 24 * time.Hour
	}
	if options.PairingTTL <= 0 {
		options.PairingTTL = 10 * time.Minute
	}
	if options.DownloadTokenTTL <= 0 {
		options.DownloadTokenTTL = 15 * time.Minute
	}
	if options.DeliveredDeleteDelay <= 0 {
		options.DeliveredDeleteDelay = time.Hour
	}
	if err := ensureDir(options.DataDir); err != nil {
		return nil, err
	}
	filesDir := filepath.Join(options.DataDir, "files")
	if err := ensureDir(filesDir); err != nil {
		return nil, err
	}
	s := &Store{
		statePath:             filepath.Join(options.DataDir, "state.json"),
		filesDir:              filesDir,
		fileTTL:               options.FileTTL,
		metadataTTL:           options.MetadataTTL,
		pairingTTL:            options.PairingTTL,
		downloadTokenTTL:      options.DownloadTokenTTL,
		deliveredDeleteDelay:  options.DeliveredDeleteDelay,
		maxFileBytes:          options.MaxFileBytes,
		logger:                options.Logger,
		lastProgressPersist:   make(map[string]time.Time),
		lastDownloadedPersist: make(map[string]time.Time),
	}
	s.state = emptyPersistentState()
	if err := readJSON(s.statePath, &s.state); err != nil && !os.IsNotExist(err) {
		corrupt := s.statePath + ".corrupt-" + time.Now().Format("20060102-150405")
		_ = os.Rename(s.statePath, corrupt)
		if s.logger != nil {
			s.logger.Printf("state.json inválido; se movió a %s: %v", corrupt, err)
		}
		s.state = emptyPersistentState()
	}
	s.ensureMapsLocked()
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func emptyPersistentState() PersistentState {
	return PersistentState{
		Version:        1,
		Devices:        make(map[string]Device),
		Links:          make(map[string]Link),
		Pairings:       make(map[string]Pairing),
		Transfers:      make(map[string]Transfer),
		DownloadTokens: make(map[string]DownloadToken),
	}
}

func (s *Store) ensureMapsLocked() {
	if s.state.Version == 0 {
		s.state.Version = 1
	}
	if s.state.Devices == nil {
		s.state.Devices = make(map[string]Device)
	}
	if s.state.Links == nil {
		s.state.Links = make(map[string]Link)
	}
	if s.state.Pairings == nil {
		s.state.Pairings = make(map[string]Pairing)
	}
	if s.state.Transfers == nil {
		s.state.Transfers = make(map[string]Transfer)
	}
	if s.state.DownloadTokens == nil {
		s.state.DownloadTokens = make(map[string]DownloadToken)
	}
}

func (s *Store) persistLocked() error {
	return writeJSONAtomic(s.statePath, &s.state)
}

func (s *Store) RegisterDevice(id, token, name, platform string) (Device, error) {
	id = strings.TrimSpace(id)
	token = strings.TrimSpace(token)
	name = strings.TrimSpace(name)
	platform = strings.TrimSpace(platform)
	if id == "" || len(id) > 128 || token == "" || len(token) < 24 || len(token) > 256 {
		return Device{}, fmt.Errorf("datos de dispositivo inválidos")
	}
	if name == "" {
		name = "Dispositivo"
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	if platform == "" {
		platform = "web"
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.state.Devices[id]; ok {
		if !secureTokenEqual(existing.TokenHash, token) {
			return Device{}, ErrUnauthorized
		}
		existing.Name = name
		existing.Platform = platform
		existing.LastSeenAt = now
		s.state.Devices[id] = existing
		if err := s.persistLocked(); err != nil {
			return Device{}, err
		}
		return existing, nil
	}
	device := Device{
		ID: id, TokenHash: tokenHash(token), Name: name, Platform: platform,
		CreatedAt: now, LastSeenAt: now,
	}
	s.state.Devices[id] = device
	if err := s.persistLocked(); err != nil {
		return Device{}, err
	}
	return device, nil
}

func (s *Store) Authenticate(id, token string) (Device, error) {
	s.mu.RLock()
	device, ok := s.state.Devices[id]
	s.mu.RUnlock()
	if !ok || !secureTokenEqual(device.TokenHash, token) {
		return Device{}, ErrUnauthorized
	}
	// LastSeen is intentionally not persisted for every API call.
	now := time.Now().UTC()
	if now.Sub(device.LastSeenAt) > 30*time.Second {
		s.mu.Lock()
		if current, exists := s.state.Devices[id]; exists && secureTokenEqual(current.TokenHash, token) {
			current.LastSeenAt = now
			s.state.Devices[id] = current
			device = current
			_ = s.persistLocked()
		}
		s.mu.Unlock()
	}
	return device, nil
}

func linkKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func (s *Store) IsLinked(a, b string) bool {
	if a == b {
		return true
	}
	s.mu.RLock()
	_, ok := s.state.Links[linkKey(a, b)]
	s.mu.RUnlock()
	return ok
}

func (s *Store) CreatePairing(deviceID string) (Pairing, error) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Devices[deviceID]; !ok {
		return Pairing{}, ErrUnauthorized
	}
	// Only keep one live code per creator.
	for key, p := range s.state.Pairings {
		if p.CreatorDeviceID == deviceID && p.UsedAt == nil && p.ExpiresAt.After(now) {
			delete(s.state.Pairings, key)
		}
	}
	var code string
	for i := 0; i < 20; i++ {
		code = pairingCode()
		if _, exists := s.state.Pairings[code]; !exists {
			break
		}
	}
	pairing := Pairing{
		ID: randomID("pair"), Code: code, CreatorDeviceID: deviceID,
		CreatedAt: now, ExpiresAt: now.Add(s.pairingTTL),
	}
	s.state.Pairings[code] = pairing
	if err := s.persistLocked(); err != nil {
		return Pairing{}, err
	}
	return pairing, nil
}

func (s *Store) CurrentPairing(deviceID string) *Pairing {
	now := time.Now().UTC()
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *Pairing
	for _, p := range s.state.Pairings {
		if p.CreatorDeviceID != deviceID || p.UsedAt != nil || !p.ExpiresAt.After(now) {
			continue
		}
		candidate := p
		if latest == nil || candidate.CreatedAt.After(latest.CreatedAt) {
			latest = &candidate
		}
	}
	return latest
}

func (s *Store) JoinPairing(joinerID, rawCode string) (PeerView, error) {
	code := normalizePairingCode(rawCode)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	joiner, ok := s.state.Devices[joinerID]
	if !ok {
		return PeerView{}, ErrUnauthorized
	}
	pairing, ok := s.state.Pairings[code]
	if !ok {
		return PeerView{}, ErrNotFound
	}
	if pairing.UsedAt != nil {
		return PeerView{}, ErrConflict
	}
	if !pairing.ExpiresAt.After(now) {
		return PeerView{}, ErrExpired
	}
	if pairing.CreatorDeviceID == joinerID {
		return PeerView{}, errors.New("no podés vincular el dispositivo consigo mismo")
	}
	creator, ok := s.state.Devices[pairing.CreatorDeviceID]
	if !ok {
		return PeerView{}, ErrNotFound
	}
	key := linkKey(joinerID, creator.ID)
	if _, exists := s.state.Links[key]; !exists {
		s.state.Links[key] = Link{ID: randomID("link"), DeviceA: minString(joinerID, creator.ID), DeviceB: maxString(joinerID, creator.ID), CreatedAt: now}
	}
	pairing.UsedAt = &now
	pairing.UsedByDeviceID = joinerID
	s.state.Pairings[code] = pairing
	joiner.LastSeenAt = now
	s.state.Devices[joinerID] = joiner
	if err := s.persistLocked(); err != nil {
		return PeerView{}, err
	}
	return peerFromDevice(creator, now), nil
}

func minString(a, b string) string {
	if a < b {
		return a
	}
	return b
}

func maxString(a, b string) string {
	if a > b {
		return a
	}
	return b
}

func peerFromDevice(device Device, now time.Time) PeerView {
	return PeerView{
		ID: device.ID, Name: device.Name, Platform: device.Platform,
		LastSeenAt: device.LastSeenAt, Online: now.Sub(device.LastSeenAt) < 90*time.Second,
	}
}

func (s *Store) ListPeers(deviceID string) []PeerView {
	now := time.Now().UTC()
	s.mu.RLock()
	defer s.mu.RUnlock()
	peers := make([]PeerView, 0)
	for _, link := range s.state.Links {
		var peerID string
		switch deviceID {
		case link.DeviceA:
			peerID = link.DeviceB
		case link.DeviceB:
			peerID = link.DeviceA
		default:
			continue
		}
		if device, ok := s.state.Devices[peerID]; ok {
			peers = append(peers, peerFromDevice(device, now))
		}
	}
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Online != peers[j].Online {
			return peers[i].Online
		}
		return strings.ToLower(peers[i].Name) < strings.ToLower(peers[j].Name)
	})
	return peers
}

func (s *Store) Unlink(deviceID, peerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := linkKey(deviceID, peerID)
	if _, ok := s.state.Links[key]; !ok {
		return ErrNotFound
	}
	delete(s.state.Links, key)
	return s.persistLocked()
}

func (s *Store) CreateTransfer(senderID, receiverID, filename, mime string, size int64, sourceLabel, storageKind, localPath string) (Transfer, error) {
	if senderID == "" || receiverID == "" || senderID == receiverID {
		return Transfer{}, errors.New("remitente o destinatario inválido")
	}
	if size < 0 {
		return Transfer{}, errors.New("tamaño inválido")
	}
	if s.maxFileBytes > 0 && size > s.maxFileBytes {
		return Transfer{}, fmt.Errorf("el servidor admite como máximo %d bytes", s.maxFileBytes)
	}
	filename = sanitizeFilename(filename)
	if storageKind == "" {
		storageKind = "spool"
	}
	if storageKind != "spool" && storageKind != "linked" {
		return Transfer{}, errors.New("almacenamiento inválido")
	}
	if storageKind == "linked" {
		info, err := os.Stat(localPath)
		if err != nil || !info.Mode().IsRegular() {
			return Transfer{}, errors.New("el archivo local no existe o no es regular")
		}
		size = info.Size()
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Devices[senderID]; !ok {
		return Transfer{}, ErrUnauthorized
	}
	if _, ok := s.state.Devices[receiverID]; !ok {
		return Transfer{}, ErrNotFound
	}
	if _, ok := s.state.Links[linkKey(senderID, receiverID)]; !ok {
		return Transfer{}, ErrNotLinked
	}
	id := randomID("tx")
	storedPath := filepath.Join(s.filesDir, id+".bin")
	status := StatusPending
	uploaded := int64(0)
	if storageKind == "linked" {
		status = StatusReady
		uploaded = size
		storedPath = ""
	}
	transfer := Transfer{
		ID: id, SenderDeviceID: senderID, ReceiverDeviceID: receiverID,
		Filename: filename, Size: size, MIME: strings.TrimSpace(mime),
		Status: status, StorageKind: storageKind, StoredPath: storedPath,
		LocalSourcePath: localPath, SourceLabel: sourceLabel,
		UploadedBytes: uploaded, CreatedAt: now, UpdatedAt: now,
		ExpiresAt: now.Add(s.fileTTL),
	}
	if d, ok := s.state.Devices[senderID]; ok {
		transfer.SenderDisplayName = d.Name
	}
	if d, ok := s.state.Devices[receiverID]; ok {
		transfer.ReceiverName = d.Name
	}
	s.state.Transfers[id] = transfer
	if err := s.persistLocked(); err != nil {
		return Transfer{}, err
	}
	return transfer, nil
}

func (s *Store) GetTransfer(id string) (Transfer, error) {
	s.mu.RLock()
	transfer, ok := s.state.Transfers[id]
	s.mu.RUnlock()
	if !ok {
		return Transfer{}, ErrNotFound
	}
	return transfer, nil
}

func (s *Store) GetTransferForDevice(id, deviceID string) (Transfer, error) {
	transfer, err := s.GetTransfer(id)
	if err != nil {
		return Transfer{}, err
	}
	if transfer.SenderDeviceID != deviceID && transfer.ReceiverDeviceID != deviceID {
		return Transfer{}, ErrUnauthorized
	}
	return transfer, nil
}

func (s *Store) BeginUpload(id, senderID string) (Transfer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	transfer, ok := s.state.Transfers[id]
	if !ok {
		return Transfer{}, ErrNotFound
	}
	if transfer.SenderDeviceID != senderID {
		return Transfer{}, ErrUnauthorized
	}
	if transfer.StorageKind != "spool" {
		return Transfer{}, ErrConflict
	}
	if transfer.Status != StatusPending && transfer.Status != StatusError {
		return Transfer{}, ErrConflict
	}
	transfer.Status = StatusUploading
	transfer.Error = ""
	transfer.UploadedBytes = 0
	transfer.UpdatedAt = time.Now().UTC()
	s.state.Transfers[id] = transfer
	if err := s.persistLocked(); err != nil {
		return Transfer{}, err
	}
	return transfer, nil
}

func (s *Store) UpdateUploadProgress(id string, bytes int64, force bool) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	transfer, ok := s.state.Transfers[id]
	if !ok {
		return
	}
	transfer.UploadedBytes = bytes
	transfer.UpdatedAt = now
	s.state.Transfers[id] = transfer
	last := s.lastProgressPersist[id]
	if force || now.Sub(last) > 2*time.Second {
		_ = s.persistLocked()
		s.lastProgressPersist[id] = now
	}
}

func (s *Store) FinishUpload(id, sha string, bytes int64) (Transfer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	transfer, ok := s.state.Transfers[id]
	if !ok {
		return Transfer{}, ErrNotFound
	}
	if transfer.Size > 0 && transfer.Size != bytes {
		transfer.Status = StatusError
		transfer.Error = fmt.Sprintf("tamaño inesperado: %d de %d bytes", bytes, transfer.Size)
		transfer.UploadedBytes = bytes
		transfer.UpdatedAt = time.Now().UTC()
		s.state.Transfers[id] = transfer
		_ = s.persistLocked()
		return transfer, errors.New(transfer.Error)
	}
	if transfer.Size == 0 {
		transfer.Size = bytes
	}
	transfer.SHA256 = sha
	transfer.UploadedBytes = bytes
	transfer.Status = StatusReady
	transfer.Error = ""
	transfer.UpdatedAt = time.Now().UTC()
	transfer.ExpiresAt = transfer.UpdatedAt.Add(s.fileTTL)
	s.state.Transfers[id] = transfer
	if err := s.persistLocked(); err != nil {
		return Transfer{}, err
	}
	return transfer, nil
}

func (s *Store) FailTransfer(id, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	transfer, ok := s.state.Transfers[id]
	if !ok {
		return
	}
	transfer.Status = StatusError
	transfer.Error = message
	transfer.UpdatedAt = time.Now().UTC()
	s.state.Transfers[id] = transfer
	_ = s.persistLocked()
}

func (s *Store) ListTransfers(deviceID, box, status string, limit int) []Transfer {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	s.mu.RLock()
	items := make([]Transfer, 0)
	for _, transfer := range s.state.Transfers {
		switch box {
		case "inbox":
			if transfer.ReceiverDeviceID != deviceID {
				continue
			}
		case "outbox":
			if transfer.SenderDeviceID != deviceID {
				continue
			}
		default:
			if transfer.SenderDeviceID != deviceID && transfer.ReceiverDeviceID != deviceID {
				continue
			}
		}
		if status != "" && transfer.Status != status {
			continue
		}
		items = append(items, transfer)
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (s *Store) CreateDownloadToken(transferID, receiverID string) (string, DownloadToken, error) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	transfer, ok := s.state.Transfers[transferID]
	if !ok {
		return "", DownloadToken{}, ErrNotFound
	}
	if transfer.ReceiverDeviceID != receiverID {
		return "", DownloadToken{}, ErrUnauthorized
	}
	if transfer.Status != StatusReady && transfer.Status != StatusDelivered {
		return "", DownloadToken{}, ErrConflict
	}
	raw := randomToken(32)
	token := DownloadToken{
		TokenHash: tokenHash(raw), TransferID: transferID, ReceiverDeviceID: receiverID,
		CreatedAt: now, ExpiresAt: now.Add(s.downloadTokenTTL),
	}
	s.state.DownloadTokens[token.TokenHash] = token
	if err := s.persistLocked(); err != nil {
		return "", DownloadToken{}, err
	}
	return raw, token, nil
}

func (s *Store) ResolveDownloadToken(raw string) (Transfer, error) {
	hash := tokenHash(raw)
	now := time.Now().UTC()
	s.mu.RLock()
	token, ok := s.state.DownloadTokens[hash]
	if !ok || !token.ExpiresAt.After(now) {
		s.mu.RUnlock()
		if ok {
			return Transfer{}, ErrExpired
		}
		return Transfer{}, ErrUnauthorized
	}
	transfer, ok := s.state.Transfers[token.TransferID]
	s.mu.RUnlock()
	if !ok {
		return Transfer{}, ErrNotFound
	}
	return transfer, nil
}

func (s *Store) UpdateDownloaded(id string, bytes int64, force bool) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	transfer, ok := s.state.Transfers[id]
	if !ok {
		return
	}
	if bytes > transfer.DownloadedBytes {
		transfer.DownloadedBytes = bytes
	}
	transfer.UpdatedAt = now
	s.state.Transfers[id] = transfer
	last := s.lastDownloadedPersist[id]
	if force || now.Sub(last) > 2*time.Second {
		_ = s.persistLocked()
		s.lastDownloadedPersist[id] = now
	}
}

func (s *Store) MarkDelivered(id, receiverID, destinationLabel string) (Transfer, error) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	transfer, ok := s.state.Transfers[id]
	if !ok {
		return Transfer{}, ErrNotFound
	}
	if transfer.ReceiverDeviceID != receiverID {
		return Transfer{}, ErrUnauthorized
	}
	if transfer.Status != StatusReady && transfer.Status != StatusDelivered {
		return Transfer{}, ErrConflict
	}
	transfer.Status = StatusDelivered
	transfer.DestinationLabel = strings.TrimSpace(destinationLabel)
	transfer.DeliveredAt = &now
	transfer.UpdatedAt = now
	deleteAfter := now.Add(s.deliveredDeleteDelay)
	transfer.DeleteAfter = &deleteAfter
	if transfer.DownloadedBytes < transfer.Size {
		transfer.DownloadedBytes = transfer.Size
	}
	s.state.Transfers[id] = transfer
	if err := s.persistLocked(); err != nil {
		return Transfer{}, err
	}
	return transfer, nil
}

func (s *Store) CancelTransfer(id, deviceID string) error {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	transfer, ok := s.state.Transfers[id]
	if !ok {
		return ErrNotFound
	}
	if transfer.SenderDeviceID != deviceID && transfer.ReceiverDeviceID != deviceID {
		return ErrUnauthorized
	}
	transfer.Status = StatusCancelled
	transfer.UpdatedAt = now
	deleteAfter := now.Add(5 * time.Minute)
	transfer.DeleteAfter = &deleteAfter
	s.state.Transfers[id] = transfer
	return s.persistLocked()
}

func (s *Store) TransferFilePath(transfer Transfer) (string, error) {
	switch transfer.StorageKind {
	case "linked":
		if transfer.LocalSourcePath == "" {
			return "", ErrNotFound
		}
		path := filepath.Clean(transfer.LocalSourcePath)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return "", ErrNotFound
		}
		return path, nil
	case "spool":
		path := filepath.Clean(transfer.StoredPath)
		base := filepath.Clean(s.filesDir) + string(os.PathSeparator)
		if !strings.HasPrefix(path+string(os.PathSeparator), base) && path != filepath.Clean(s.filesDir) {
			return "", ErrUnauthorized
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return "", ErrNotFound
		}
		return path, nil
	default:
		return "", ErrNotFound
	}
}

func (s *Store) Cleanup() (deletedFiles, deletedMetadata int) {
	now := time.Now().UTC()
	var filesToDelete []string
	s.mu.Lock()
	for code, pairing := range s.state.Pairings {
		if pairing.ExpiresAt.Before(now.Add(-time.Hour)) || (pairing.UsedAt != nil && pairing.UsedAt.Before(now.Add(-time.Hour))) {
			delete(s.state.Pairings, code)
		}
	}
	for hash, token := range s.state.DownloadTokens {
		if token.ExpiresAt.Before(now) {
			delete(s.state.DownloadTokens, hash)
		}
	}
	for id, transfer := range s.state.Transfers {
		deleteFile := false
		if transfer.DeleteAfter != nil && transfer.DeleteAfter.Before(now) {
			deleteFile = true
		}
		if transfer.ExpiresAt.Before(now) {
			deleteFile = true
		}
		if deleteFile && transfer.StorageKind == "spool" && transfer.StoredPath != "" {
			filesToDelete = append(filesToDelete, transfer.StoredPath)
			transfer.StoredPath = ""
			if transfer.Status == StatusReady {
				transfer.Status = StatusError
				transfer.Error = "el archivo temporal venció"
			}
			s.state.Transfers[id] = transfer
		}
		cutoff := now.Add(-s.metadataTTL)
		if transfer.CreatedAt.Before(cutoff) && (transfer.Status == StatusDelivered || transfer.Status == StatusCancelled || transfer.Status == StatusError) {
			delete(s.state.Transfers, id)
			deletedMetadata++
		}
	}
	_ = s.persistLocked()
	s.mu.Unlock()
	for _, path := range filesToDelete {
		if err := os.Remove(path); err == nil || os.IsNotExist(err) {
			deletedFiles++
		}
	}
	return deletedFiles, deletedMetadata
}

func (s *Store) Snapshot() PersistentState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copyState := emptyPersistentState()
	copyState.Version = s.state.Version
	for k, v := range s.state.Devices {
		copyState.Devices[k] = v
	}
	for k, v := range s.state.Links {
		copyState.Links[k] = v
	}
	for k, v := range s.state.Pairings {
		copyState.Pairings[k] = v
	}
	for k, v := range s.state.Transfers {
		copyState.Transfers[k] = v
	}
	for k, v := range s.state.DownloadTokens {
		copyState.DownloadTokens[k] = v
	}
	return copyState
}
