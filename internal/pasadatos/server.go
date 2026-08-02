package pasadatos

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web web/assets/*
var embeddedWeb embed.FS

type HubServer struct {
	options    ServerOptions
	store      *Store
	mux        *http.ServeMux
	httpServer *http.Server
	webFS      fs.FS
	logger     Logger
	desktop    *DesktopAgent
	cleanupEnd chan struct{}
	startOnce  sync.Once
}

func NewHubServer(options ServerOptions) (*HubServer, error) {
	if strings.TrimSpace(options.ListenAddress) == "" {
		options.ListenAddress = "0.0.0.0:8088"
	}
	if strings.TrimSpace(options.DataDir) == "" {
		options.DataDir = "./data"
	}
	if options.Logger == nil {
		options.Logger = log.New(os.Stdout, "[pasaDATOS] ", log.LstdFlags|log.LUTC)
	}
	store, err := NewStore(options)
	if err != nil {
		return nil, err
	}
	webFS, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		return nil, err
	}
	s := &HubServer{
		options: options, store: store, mux: http.NewServeMux(), webFS: webFS,
		logger: options.Logger, cleanupEnd: make(chan struct{}),
	}
	s.routes()
	s.httpServer = &http.Server{
		Addr:              options.ListenAddress,
		Handler:           s.securityHeaders(s.mux),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	return s, nil
}

func (s *HubServer) AttachDesktop(agent *DesktopAgent) {
	s.desktop = agent
	s.installNativeRoutes()
}

func (s *HubServer) Store() *Store { return s.store }

func (s *HubServer) Handler() http.Handler { return s.httpServer.Handler }

func (s *HubServer) StartCleanupLoop() {
	s.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					files, metadata := s.store.Cleanup()
					if files > 0 || metadata > 0 {
						s.logger.Printf("limpieza: %d archivos temporales y %d registros", files, metadata)
					}
				case <-s.cleanupEnd:
					return
				}
			}
		}()
	})
}

func (s *HubServer) ListenAndServe() error {
	s.StartCleanupLoop()
	s.logger.Printf("%s %s escuchando en %s", AppName, AppVersion, s.options.ListenAddress)
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *HubServer) Serve(listener net.Listener) error {
	s.StartCleanupLoop()
	return s.httpServer.Serve(listener)
}

func (s *HubServer) Shutdown() error {
	select {
	case <-s.cleanupEnd:
	default:
		close(s.cleanupEnd)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

func (s *HubServer) routes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	s.mux.HandleFunc("PUT /api/v1/devices/{id}", s.handleRegisterDevice)
	s.mux.HandleFunc("GET /api/v1/me/peers", s.handleListPeers)
	s.mux.HandleFunc("DELETE /api/v1/me/peers/{peerID}", s.handleUnlink)
	s.mux.HandleFunc("POST /api/v1/pairings", s.handleCreatePairing)
	s.mux.HandleFunc("POST /api/v1/pairings/join", s.handleJoinPairing)
	s.mux.HandleFunc("POST /api/v1/transfers", s.handleCreateTransfer)
	s.mux.HandleFunc("GET /api/v1/transfers", s.handleListTransfers)
	s.mux.HandleFunc("GET /api/v1/transfers/{id}", s.handleGetTransfer)
	s.mux.HandleFunc("PUT /api/v1/transfers/{id}/content", s.handleUploadContent)
	s.mux.HandleFunc("GET /api/v1/transfers/{id}/content", s.handleAuthenticatedDownload)
	s.mux.HandleFunc("POST /api/v1/transfers/{id}/download-token", s.handleCreateDownloadToken)
	s.mux.HandleFunc("POST /api/v1/transfers/{id}/received", s.handleMarkReceived)
	s.mux.HandleFunc("DELETE /api/v1/transfers/{id}", s.handleCancelTransfer)
	s.mux.HandleFunc("GET /api/v1/download/{token}", s.handlePublicDownload)
	s.mux.HandleFunc("HEAD /api/v1/download/{token}", s.handlePublicDownload)
	s.mux.HandleFunc("GET /desktop/", s.handleDesktopIndex)
	s.mux.HandleFunc("GET /", s.handleStatic)
}

func (s *HubServer) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self' http: https:; worker-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *HubServer) authenticate(r *http.Request) (Device, error) {
	id := strings.TrimSpace(r.Header.Get("X-Device-ID"))
	token := bearerToken(r)
	if id == "" || token == "" {
		return Device{}, ErrUnauthorized
	}
	return s.store.Authenticate(id, token)
}

func (s *HubServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "service": AppName, "version": AppVersion,
		"time": time.Now().UTC(), "desktop_mode": s.options.DesktopMode,
		"public_url": strings.TrimRight(s.options.PublicURL, "/"),
	})
}

func (s *HubServer) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if err := decodeJSONBody(r, &body, 64*1024); err != nil {
		writeAPIError(w, err)
		return
	}
	device, err := s.store.RegisterDevice(r.PathValue("id"), body.Token, body.Name, body.Platform)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	device.TokenHash = ""
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device": device})
}

func (s *HubServer) handleListPeers(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "peers": s.store.ListPeers(device.ID)})
}

func (s *HubServer) handleUnlink(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	if err := s.store.Unlink(device.ID, r.PathValue("peerID")); err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HubServer) handleCreatePairing(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	pairing, err := s.store.CreatePairing(device.ID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	joinURL := s.publicJoinURL(r, pairing.Code)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "pairing": pairing, "join_url": joinURL})
}

func (s *HubServer) handleJoinPairing(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := decodeJSONBody(r, &body, 32*1024); err != nil {
		writeAPIError(w, err)
		return
	}
	peer, err := s.store.JoinPairing(device.ID, body.Code)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "peer": peer})
}

func (s *HubServer) publicJoinURL(r *http.Request, code string) string {
	base := strings.TrimRight(s.options.PublicURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		host := r.Host
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); forwarded != "" {
			host = forwarded
		}
		base = scheme + "://" + host
	}
	return base + "/?pair=" + code
}

func (s *HubServer) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	var body struct {
		ReceiverID string `json:"receiver_id"`
		Filename   string `json:"filename"`
		Size       int64  `json:"size"`
		MIME       string `json:"mime"`
		Source     string `json:"source_label"`
	}
	if err := decodeJSONBody(r, &body, 128*1024); err != nil {
		writeAPIError(w, err)
		return
	}
	transfer, err := s.store.CreateTransfer(device.ID, body.ReceiverID, body.Filename, body.MIME, body.Size, body.Source, "spool", "")
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "transfer": transfer})
}

func (s *HubServer) handleListTransfers(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	items := s.store.ListTransfers(device.ID, r.URL.Query().Get("box"), r.URL.Query().Get("status"), intQuery(r, "limit", 100))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transfers": items})
}

func (s *HubServer) handleGetTransfer(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	transfer, err := s.store.GetTransferForDevice(r.PathValue("id"), device.ID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transfer": transfer})
}

func (s *HubServer) handleUploadContent(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	transfer, err := s.store.BeginUpload(r.PathValue("id"), device.ID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	defer r.Body.Close()
	if r.ContentLength < 0 {
		s.store.FailTransfer(transfer.ID, "se requiere Content-Length")
		writeJSON(w, http.StatusLengthRequired, map[string]any{"ok": false, "error": "length_required", "message": "se requiere Content-Length"})
		return
	}
	if s.options.MaxFileBytes > 0 && r.ContentLength > s.options.MaxFileBytes {
		s.store.FailTransfer(transfer.ID, "archivo demasiado grande para este servidor")
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": "too_large", "message": "archivo demasiado grande para este servidor"})
		return
	}
	if transfer.Size > 0 && transfer.Size != r.ContentLength {
		message := fmt.Sprintf("Content-Length %d no coincide con %d", r.ContentLength, transfer.Size)
		s.store.FailTransfer(transfer.ID, message)
		writeAPIError(w, errors.New(message))
		return
	}
	written, sha, copyErr := copyFileWithProgress(transfer.StoredPath, r.Body, r.ContentLength, func(done int64) {
		s.store.UpdateUploadProgress(transfer.ID, done, false)
	})
	if copyErr != nil {
		s.store.FailTransfer(transfer.ID, copyErr.Error())
		writeAPIError(w, copyErr)
		return
	}
	transfer, err = s.store.FinishUpload(transfer.ID, sha, written)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	if s.options.DesktopReceiveCallback != nil && transfer.ReceiverDeviceID == s.options.DesktopDeviceID {
		go s.options.DesktopReceiveCallback(transfer.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transfer": transfer})
}

func (s *HubServer) handleCreateDownloadToken(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	raw, token, err := s.store.CreateDownloadToken(r.PathValue("id"), device.ID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	url := "/api/v1/download/" + raw
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "url": url, "expires_at": token.ExpiresAt})
}

func (s *HubServer) handleMarkReceived(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	var body struct {
		Destination string `json:"destination_label"`
	}
	if err := decodeJSONBody(r, &body, 64*1024); err != nil {
		writeAPIError(w, err)
		return
	}
	transfer, err := s.store.MarkDelivered(r.PathValue("id"), device.ID, body.Destination)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transfer": transfer})
}

func (s *HubServer) handleCancelTransfer(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	if err := s.store.CancelTransfer(r.PathValue("id"), device.ID); err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HubServer) handleAuthenticatedDownload(w http.ResponseWriter, r *http.Request) {
	device, err := s.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	transfer, err := s.store.GetTransferForDevice(r.PathValue("id"), device.ID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	if transfer.ReceiverDeviceID != device.ID {
		writeAPIError(w, ErrUnauthorized)
		return
	}
	s.serveTransferFile(w, r, transfer)
}

func (s *HubServer) handlePublicDownload(w http.ResponseWriter, r *http.Request) {
	transfer, err := s.store.ResolveDownloadToken(r.PathValue("token"))
	if err != nil {
		writeAPIError(w, err)
		return
	}
	s.serveTransferFile(w, r, transfer)
}

func (s *HubServer) serveTransferFile(w http.ResponseWriter, r *http.Request, transfer Transfer) {
	if transfer.Status != StatusReady && transfer.Status != StatusDelivered {
		writeAPIError(w, ErrConflict)
		return
	}
	filePath, err := s.store.TransferFilePath(transfer)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		writeAPIError(w, ErrNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeAPIError(w, err)
		return
	}
	size := info.Size()
	start, end, partial, err := parseByteRange(r.Header.Get("Range"), size)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		writeJSON(w, http.StatusRequestedRangeNotSatisfiable, map[string]any{"ok": false, "error": "range_not_satisfiable", "message": err.Error()})
		return
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		writeAPIError(w, err)
		return
	}
	length := end - start + 1
	contentType := transfer.MIME
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = mime.TypeByExtension(filepath.Ext(transfer.Filename))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition(transfer.Filename))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if r.Method == http.MethodHead {
		return
	}
	buf := make([]byte, 1024*1024)
	remaining := length
	var sent int64
	for remaining > 0 {
		chunk := int64(len(buf))
		if remaining < chunk {
			chunk = remaining
		}
		n, readErr := file.Read(buf[:chunk])
		if n > 0 {
			wn, writeErr := w.Write(buf[:n])
			sent += int64(wn)
			remaining -= int64(wn)
			s.store.UpdateDownloaded(transfer.ID, start+sent, false)
			if writeErr != nil || wn != n {
				break
			}
		}
		if readErr != nil {
			break
		}
	}
	s.store.UpdateDownloaded(transfer.ID, start+sent, true)
}

func parseByteRange(header string, size int64) (start, end int64, partial bool, err error) {
	if size < 0 {
		return 0, 0, false, errors.New("tamaño inválido")
	}
	if size == 0 {
		return 0, -1, false, nil
	}
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, size - 1, false, nil
	}
	if !strings.HasPrefix(header, "bytes=") || strings.Contains(header, ",") {
		return 0, 0, false, errors.New("solo se admite un rango de bytes")
	}
	parts := strings.SplitN(strings.TrimPrefix(header, "bytes="), "-", 2)
	if len(parts) != 2 {
		return 0, 0, false, errors.New("rango inválido")
	}
	if parts[0] == "" {
		suffix, parseErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil || suffix <= 0 {
			return 0, 0, false, errors.New("rango inválido")
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, true, nil
	}
	start, parseErr := strconv.ParseInt(parts[0], 10, 64)
	if parseErr != nil || start < 0 || start >= size {
		return 0, 0, false, errors.New("inicio de rango inválido")
	}
	end = size - 1
	if parts[1] != "" {
		end, parseErr = strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil || end < start {
			return 0, 0, false, errors.New("fin de rango inválido")
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true, nil
}

func (s *HubServer) handleDesktopIndex(w http.ResponseWriter, r *http.Request) {
	if !s.options.DesktopMode {
		http.NotFound(w, r)
		return
	}
	s.serveEmbeddedFile(w, r, "desktop.html", false)
}

func (s *HubServer) handleStatic(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean(r.URL.Path)
	if strings.HasPrefix(clean, "/api/") || strings.HasPrefix(clean, "/native/") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(clean, "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	if strings.HasPrefix(name, "desktop") {
		if !s.options.DesktopMode {
			http.NotFound(w, r)
			return
		}
		name = "desktop.html"
	}
	if _, err := fs.Stat(s.webFS, name); err != nil {
		if strings.Contains(filepath.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	}
	longCache := strings.HasPrefix(name, "assets/") && !s.options.DesktopMode
	s.serveEmbeddedFile(w, r, name, longCache)
}

func (s *HubServer) serveEmbeddedFile(w http.ResponseWriter, r *http.Request, name string, longCache bool) {
	payload, err := fs.ReadFile(s.webFS, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if strings.HasSuffix(name, ".webmanifest") {
		contentType = "application/manifest+json"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if longCache {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	etagBytes := sha256.Sum256(payload)
	etag := `"` + hex.EncodeToString(etagBytes[:8]) + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}
