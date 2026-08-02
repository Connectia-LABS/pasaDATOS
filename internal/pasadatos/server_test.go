package pasadatos

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEndToEndPairUploadDownloadAndHistoryState(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	hub, err := NewHubServer(ServerOptions{
		DataDir: dataDir, ListenAddress: "127.0.0.1:0",
		FileTTL: time.Hour, MetadataTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(hub.Handler())
	defer ts.Close()

	ctx := context.Background()
	a := NewRemoteClient(ts.URL, "device_a", randomToken(32), "PC de prueba", "windows")
	b := NewRemoteClient(ts.URL, "device_b", randomToken(32), "Celular de prueba", "android")
	if _, err := a.Register(ctx); err != nil {
		t.Fatalf("register A: %v", err)
	}
	if _, err := b.Register(ctx); err != nil {
		t.Fatalf("register B: %v", err)
	}
	pair, _, err := a.CreatePairing(ctx)
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}
	peer, err := b.JoinPairing(ctx, pair.Code)
	if err != nil {
		t.Fatalf("join pairing: %v", err)
	}
	if peer.ID != a.DeviceID {
		t.Fatalf("expected peer %q, got %q", a.DeviceID, peer.ID)
	}

	payload := []byte{0x00, 0x01, 0x02, 0x7f, 0x80, 0xff, 'p', 'a', 's', 'a', 'D', 'A', 'T', 'O', 'S'}
	filePath := filepath.Join(t.TempDir(), "datos.bin")
	if err := os.WriteFile(filePath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	transfer, err := a.CreateTransfer(ctx, b.DeviceID, "datos.bin", "application/octet-stream", int64(len(payload)), filePath)
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	var lastProgress int64
	transfer, err = a.UploadFile(ctx, transfer, filePath, func(done int64) { lastProgress = done })
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if transfer.Status != StatusReady || lastProgress != int64(len(payload)) {
		t.Fatalf("unexpected upload state: status=%s progress=%d", transfer.Status, lastProgress)
	}

	inbox, err := b.ListTransfers(ctx, "inbox", StatusReady, 10)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("inbox: len=%d err=%v", len(inbox), err)
	}

	req, err := b.newRequest(ctx, http.MethodGet, "/api/v1/transfers/"+transfer.ID+"/content", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=2-6")
	resp, err := b.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rangeBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(rangeBody, payload[2:7]) {
		t.Fatalf("range response: status=%d body=%v", resp.StatusCode, rangeBody)
	}

	destination := filepath.Join(t.TempDir(), "recibido.bin")
	written, _, err := b.DownloadTo(ctx, transfer, destination, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	got, _ := os.ReadFile(destination)
	if written != int64(len(payload)) || !bytes.Equal(got, payload) {
		t.Fatalf("download differs: written=%d got=%v", written, got)
	}
	transfer, err = b.MarkReceived(ctx, transfer.ID, destination)
	if err != nil {
		t.Fatalf("mark received: %v", err)
	}
	if transfer.Status != StatusDelivered || transfer.DestinationLabel != destination {
		t.Fatalf("unexpected delivered transfer: %#v", transfer)
	}
}

func TestUnlinkedAndUnauthorizedDevicesAreRejected(t *testing.T) {
	t.Parallel()
	hub, err := NewHubServer(ServerOptions{DataDir: t.TempDir(), ListenAddress: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(hub.Handler())
	defer ts.Close()
	ctx := context.Background()
	a := NewRemoteClient(ts.URL, "device_a", randomToken(32), "A", "web")
	b := NewRemoteClient(ts.URL, "device_b", randomToken(32), "B", "web")
	_, _ = a.Register(ctx)
	_, _ = b.Register(ctx)
	_, err = a.CreateTransfer(ctx, b.DeviceID, "x.txt", "text/plain", 1, "test")
	var apiErr *APIError
	if err == nil || !errorsAs(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("expected 403 not_linked, got %v", err)
	}
	bad := NewRemoteClient(ts.URL, a.DeviceID, randomToken(32), "Intruso", "web")
	if _, err := bad.ListPeers(ctx); err == nil {
		t.Fatal("expected unauthorized error")
	}
}

func TestFilenameAndCollisionSafety(t *testing.T) {
	if got := sanitizeFilename(`../mal:<dato>?.txt`); got != "mal__dato__.txt" {
		t.Fatalf("unexpected sanitized filename: %q", got)
	}
	folder := t.TempDir()
	first := collisionSafePath(folder, "archivo.txt")
	if err := os.WriteFile(first, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := collisionSafePath(folder, "archivo.txt")
	if filepath.Base(second) != "archivo (1).txt" {
		t.Fatalf("unexpected collision path: %q", second)
	}
}

// Small wrapper keeps the test source compatible with the supported Go baseline.
func errorsAs(err error, target any) bool {
	return errors.As(err, target)
}
