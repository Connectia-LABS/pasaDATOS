package pasadatos

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *HubServer) installNativeRoutes() {
	if s.desktop == nil {
		return
	}
	s.mux.HandleFunc("GET /native/state", s.nativeOnly(s.handleNativeState))
	s.mux.HandleFunc("POST /native/pick-files", s.nativeOnly(s.handleNativePickFiles))
	s.mux.HandleFunc("POST /native/pick-folder", s.nativeOnly(s.handleNativePickFolder))
	s.mux.HandleFunc("POST /native/stage/{filename}", s.nativeOnly(s.handleNativeStage))
	s.mux.HandleFunc("POST /native/clear-selection", s.nativeOnly(s.handleNativeClearSelection))
	s.mux.HandleFunc("POST /native/send", s.nativeOnly(s.handleNativeSend))
	s.mux.HandleFunc("POST /native/pairing", s.nativeOnly(s.handleNativePairing))
	s.mux.HandleFunc("POST /native/join", s.nativeOnly(s.handleNativeJoin))
	s.mux.HandleFunc("DELETE /native/peers/{peerID}", s.nativeOnly(s.handleNativeUnlink))
	s.mux.HandleFunc("POST /native/settings", s.nativeOnly(s.handleNativeSettings))
	s.mux.HandleFunc("POST /native/open", s.nativeOnly(s.handleNativeOpen))
	s.mux.HandleFunc("POST /native/open-receive-folder", s.nativeOnly(s.handleNativeOpenReceiveFolder))
	s.mux.HandleFunc("POST /native/receive/{transferID}", s.nativeOnly(s.handleNativeReceive))
	s.mux.HandleFunc("DELETE /native/history", s.nativeOnly(s.handleNativeClearHistory))
	s.mux.HandleFunc("POST /native/exit", s.nativeOnly(s.handleNativeExit))
}

func (s *HubServer) nativeOnly(next http.HandlerFunc) http.HandlerFunc {
	expectedHash := tokenHash(s.desktop.NativeToken())
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRemote(r.RemoteAddr) {
			writeAPIError(w, ErrUnauthorized)
			return
		}
		token := strings.TrimSpace(r.Header.Get("X-pasaDATOS-Native"))
		if token == "" || !secureTokenEqual(expectedHash, token) {
			writeAPIError(w, ErrUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *HubServer) handleNativeState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": s.desktop.BuildState()})
}

func (s *HubServer) handleNativePickFiles(w http.ResponseWriter, _ *http.Request) {
	files, err := s.desktop.SelectFiles()
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "files": files})
}

func (s *HubServer) handleNativePickFolder(w http.ResponseWriter, _ *http.Request) {
	folder, err := s.desktop.SelectReceiveFolder()
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folder": folder})
}

func (s *HubServer) handleNativeStage(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength < 0 {
		writeJSON(w, http.StatusLengthRequired, map[string]any{"ok": false, "error": "length_required", "message": "se requiere Content-Length"})
		return
	}
	defer r.Body.Close()
	file, err := s.desktop.StageFile(r.PathValue("filename"), r.ContentLength, io.LimitReader(r.Body, r.ContentLength))
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "file": file})
}

func (s *HubServer) handleNativeClearSelection(w http.ResponseWriter, _ *http.Request) {
	s.desktop.ClearSelected()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HubServer) handleNativeSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode   string   `json:"mode"`
		PeerID string   `json:"peer_id"`
		Paths  []string `json:"paths"`
	}
	if err := decodeJSONBody(r, &body, 4<<20); err != nil {
		writeAPIError(w, err)
		return
	}
	jobs, err := s.desktop.SendFiles(context.Background(), body.Mode, body.PeerID, body.Paths)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "jobs": jobs})
}

func (s *HubServer) handleNativePairing(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := decodeJSONBody(r, &body, 32*1024); err != nil {
		writeAPIError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pairing, joinURL, err := s.desktop.CreatePairing(ctx, body.Mode)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "pairing": pairing, "join_url": joinURL})
}

func (s *HubServer) handleNativeJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
		Code string `json:"code"`
	}
	if err := decodeJSONBody(r, &body, 32*1024); err != nil {
		writeAPIError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	peer, err := s.desktop.JoinPairing(ctx, body.Mode, body.Code)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "peer": peer})
}

func (s *HubServer) handleNativeUnlink(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.desktop.Unlink(ctx, mode, r.PathValue("peerID")); err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HubServer) handleNativeSettings(w http.ResponseWriter, r *http.Request) {
	var body DesktopSettingsUpdate
	if err := decodeJSONBody(r, &body, 128*1024); err != nil {
		writeAPIError(w, err)
		return
	}
	cfg, err := s.desktop.UpdateSettings(body)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": cfg})
}

func (s *HubServer) handleNativeOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSONBody(r, &body, 128*1024); err != nil {
		writeAPIError(w, err)
		return
	}
	if err := s.desktop.OpenPath(body.Path); err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HubServer) handleNativeOpenReceiveFolder(w http.ResponseWriter, _ *http.Request) {
	if err := s.desktop.OpenReceiveFolder(); err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HubServer) handleNativeReceive(w http.ResponseWriter, r *http.Request) {
	transferID := strings.TrimSpace(r.PathValue("transferID"))
	if transferID == "" {
		writeAPIError(w, errors.New("transferencia inválida"))
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "remote" {
		go s.desktop.ReceiveRemoteTransfer(transferID)
	} else {
		go s.desktop.ReceiveLocalTransfer(transferID)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *HubServer) handleNativeClearHistory(w http.ResponseWriter, _ *http.Request) {
	if err := s.desktop.ClearHistory(); err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HubServer) handleNativeExit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	if s.options.DesktopExitCallback != nil {
		go s.options.DesktopExitCallback()
	}
}
