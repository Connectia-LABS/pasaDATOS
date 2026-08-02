package pasadatos

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "bad_request"
	switch {
	case errors.Is(err, ErrUnauthorized):
		status, code = http.StatusUnauthorized, "unauthorized"
	case errors.Is(err, ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, ErrNotLinked):
		status, code = http.StatusForbidden, "not_linked"
	case errors.Is(err, ErrExpired):
		status, code = http.StatusGone, "expired"
	case errors.Is(err, ErrConflict):
		status, code = http.StatusConflict, "conflict"
	}
	writeJSON(w, status, map[string]any{"ok": false, "error": code, "message": err.Error()})
}

func decodeJSONBody(r *http.Request, target any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1024 * 1024
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func intQuery(r *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
