package pasadatos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return fmt.Sprintf("error HTTP %d", e.Status)
}

type RemoteClient struct {
	BaseURL   string
	DeviceID  string
	Token     string
	Name      string
	Platform  string
	HTTP      *http.Client
	UserAgent string
}

func NewRemoteClient(baseURL, deviceID, token, name, platform string) *RemoteClient {
	return &RemoteClient{
		BaseURL: strings.TrimRight(baseURL, "/"), DeviceID: deviceID, Token: token,
		Name: name, Platform: platform,
		HTTP:      &http.Client{Timeout: 45 * time.Second},
		UserAgent: AppName + "/" + AppVersion,
	}
}

func (c *RemoteClient) endpoint(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + path
}

func (c *RemoteClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, errors.New("servidor remoto no configurado")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if c.DeviceID != "" {
		req.Header.Set("X-Device-ID", c.DeviceID)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return req, nil
}

func (c *RemoteClient) doJSON(ctx context.Context, method, path string, input any, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeAPIError(resp)
	}
	if output == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 8<<20))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("respuesta inválida del servidor: %w", err)
	}
	return nil
}

func decodeAPIError(resp *http.Response) error {
	var payload struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload)
	if payload.Message == "" {
		payload.Message = http.StatusText(resp.StatusCode)
	}
	return &APIError{Status: resp.StatusCode, Code: payload.Error, Message: payload.Message}
}

func (c *RemoteClient) Health(ctx context.Context) error {
	var response struct {
		OK bool `json:"ok"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/health", nil, &response); err != nil {
		return err
	}
	if !response.OK {
		return errors.New("el servidor no respondió correctamente")
	}
	return nil
}

func (c *RemoteClient) Register(ctx context.Context) (Device, error) {
	var response struct {
		Device Device `json:"device"`
	}
	err := c.doJSON(ctx, http.MethodPut, "/api/v1/devices/"+url.PathEscape(c.DeviceID), map[string]any{
		"token": c.Token, "name": c.Name, "platform": c.Platform,
	}, &response)
	return response.Device, err
}

func (c *RemoteClient) ListPeers(ctx context.Context) ([]PeerView, error) {
	var response struct {
		Peers []PeerView `json:"peers"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/me/peers", nil, &response)
	return response.Peers, err
}

func (c *RemoteClient) Unlink(ctx context.Context, peerID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/v1/me/peers/"+url.PathEscape(peerID), nil, nil)
}

func (c *RemoteClient) CreatePairing(ctx context.Context) (Pairing, string, error) {
	var response struct {
		Pairing Pairing `json:"pairing"`
		JoinURL string  `json:"join_url"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/pairings", map[string]any{}, &response)
	return response.Pairing, response.JoinURL, err
}

func (c *RemoteClient) JoinPairing(ctx context.Context, code string) (PeerView, error) {
	var response struct {
		Peer PeerView `json:"peer"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/pairings/join", map[string]any{"code": code}, &response)
	return response.Peer, err
}

func (c *RemoteClient) CreateTransfer(ctx context.Context, receiverID, filename, contentType string, size int64, sourceLabel string) (Transfer, error) {
	var response struct {
		Transfer Transfer `json:"transfer"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/transfers", map[string]any{
		"receiver_id": receiverID, "filename": filename, "size": size,
		"mime": contentType, "source_label": sourceLabel,
	}, &response)
	return response.Transfer, err
}

type progressReader struct {
	reader   io.Reader
	done     int64
	progress func(int64)
	last     time.Time
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	if n > 0 {
		p.done += int64(n)
		now := time.Now()
		if p.progress != nil && (now.Sub(p.last) >= 120*time.Millisecond || err == io.EOF) {
			p.progress(p.done)
			p.last = now
		}
	}
	return n, err
}

func (c *RemoteClient) UploadFile(ctx context.Context, transfer Transfer, filePath string, progress func(int64)) (Transfer, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return Transfer{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Transfer{}, err
	}
	reader := &progressReader{reader: file, progress: progress}
	req, err := c.newRequest(ctx, http.MethodPut, "/api/v1/transfers/"+url.PathEscape(transfer.ID)+"/content", reader)
	if err != nil {
		return Transfer{}, err
	}
	req.ContentLength = info.Size()
	contentType := transfer.MIME
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(filePath))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	client := *c.HTTP
	client.Timeout = 0 // large transfers may legitimately take hours
	resp, err := client.Do(req)
	if err != nil {
		return Transfer{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Transfer{}, decodeAPIError(resp)
	}
	var response struct {
		Transfer Transfer `json:"transfer"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&response); err != nil {
		return Transfer{}, err
	}
	if progress != nil {
		progress(info.Size())
	}
	return response.Transfer, nil
}

func (c *RemoteClient) ListTransfers(ctx context.Context, box, status string, limit int) ([]Transfer, error) {
	values := url.Values{}
	if box != "" {
		values.Set("box", box)
	}
	if status != "" {
		values.Set("status", status)
	}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	path := "/api/v1/transfers"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response struct {
		Transfers []Transfer `json:"transfers"`
	}
	err := c.doJSON(ctx, http.MethodGet, path, nil, &response)
	return response.Transfers, err
}

func (c *RemoteClient) GetTransfer(ctx context.Context, transferID string) (Transfer, error) {
	var response struct {
		Transfer Transfer `json:"transfer"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/transfers/"+url.PathEscape(transferID), nil, &response)
	return response.Transfer, err
}

func (c *RemoteClient) DownloadTo(ctx context.Context, transfer Transfer, destination string, progress func(int64)) (int64, string, error) {
	if err := ensureDir(filepath.Dir(destination)); err != nil {
		return 0, "", err
	}
	part := destination + ".pasadatos.part"
	var offset int64
	if info, err := os.Stat(part); err == nil && info.Mode().IsRegular() {
		offset = info.Size()
		if transfer.Size > 0 && offset > transfer.Size {
			offset = 0
			_ = os.Remove(part)
		}
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return 0, "", err
	}
	defer out.Close()
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/transfers/"+url.PathEscape(transfer.ID)+"/content", nil)
	if err != nil {
		return 0, "", err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	client := *c.HTTP
	client.Timeout = 0
	resp, err := client.Do(req)
	if err != nil {
		return offset, "", err
	}
	defer resp.Body.Close()
	if offset > 0 && resp.StatusCode == http.StatusOK {
		// Server ignored Range. Restart to avoid corrupting the destination.
		if err := out.Close(); err != nil {
			return 0, "", err
		}
		if err := os.Remove(part); err != nil && !os.IsNotExist(err) {
			return 0, "", err
		}
		return c.DownloadTo(ctx, transfer, destination, progress)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return offset, "", decodeAPIError(resp)
	}
	hash := ""
	// The complete hash is verified after the stream when the server provides one.
	reader := &progressReader{reader: resp.Body, done: offset, progress: progress}
	buf := make([]byte, 1024*1024)
	written, err := io.CopyBuffer(out, reader, buf)
	total := offset + written
	if err != nil {
		return total, hash, err
	}
	if err := out.Sync(); err != nil {
		return total, hash, err
	}
	if transfer.Size > 0 && total != transfer.Size {
		return total, hash, fmt.Errorf("descarga incompleta: %d de %d bytes", total, transfer.Size)
	}
	if err := out.Close(); err != nil {
		return total, hash, err
	}
	if err := os.Rename(part, destination); err != nil {
		return total, hash, err
	}
	if progress != nil {
		progress(total)
	}
	return total, hash, nil
}

func (c *RemoteClient) MarkReceived(ctx context.Context, transferID, destinationLabel string) (Transfer, error) {
	var response struct {
		Transfer Transfer `json:"transfer"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/transfers/"+url.PathEscape(transferID)+"/received", map[string]any{
		"destination_label": destinationLabel,
	}, &response)
	return response.Transfer, err
}

func (c *RemoteClient) Cancel(ctx context.Context, transferID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/v1/transfers/"+url.PathEscape(transferID), nil, nil)
}
