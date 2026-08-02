package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pasadatos.local/pasadatos/internal/pasadatos"
)

func main() {
	serverMode := flag.Bool("server", false, "ejecutar como relay remoto")
	background := flag.Bool("background", false, "iniciar la app sin abrir la ventana")
	noBrowser := flag.Bool("no-browser", false, "no abrir la interfaz")
	listen := flag.String("listen", "", "dirección de escucha")
	dataDirFlag := flag.String("data-dir", "", "carpeta de datos")
	publicURL := flag.String("public-url", "", "URL pública del relay")
	showVersion := flag.Bool("version", false, "mostrar versión")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s\n", pasadatos.AppName, pasadatos.AppVersion)
		return
	}

	var err error
	if *serverMode {
		err = runServer(*listen, *dataDirFlag, *publicURL)
	} else {
		err = runDesktop(*listen, *dataDirFlag, *background || *noBrowser)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pasaDATOS:", err)
		os.Exit(1)
	}
}

func runServer(listenFlag, dataFlag, publicFlag string) error {
	listen := firstNonEmpty(listenFlag, os.Getenv("PASADATOS_LISTEN"), "0.0.0.0:8088")
	dataDir := firstNonEmpty(dataFlag, os.Getenv("PASADATOS_DATA_DIR"), "/data")
	publicURL := firstNonEmpty(publicFlag, os.Getenv("PASADATOS_PUBLIC_URL"))
	fileTTL := envDurationHours("PASADATOS_FILE_TTL_HOURS", 24)
	metadataTTL := envDurationHours("PASADATOS_METADATA_TTL_HOURS", 24*30)
	maxFileBytes := envInt64("PASADATOS_MAX_FILE_BYTES", 0)
	logger := log.New(os.Stdout, "[pasaDATOS relay] ", log.LstdFlags|log.LUTC)
	server, err := pasadatos.NewHubServer(pasadatos.ServerOptions{
		ListenAddress: listen, DataDir: dataDir, PublicURL: publicURL,
		FileTTL: fileTTL, MetadataTTL: metadataTTL, MaxFileBytes: maxFileBytes, Logger: logger,
	})
	if err != nil {
		return err
	}
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	select {
	case <-sigCtx.Done():
		return server.Shutdown()
	case err := <-errCh:
		return err
	}
}

func runDesktop(listenFlag, dataFlag string, suppressWindow bool) error {
	dataDir := strings.TrimSpace(dataFlag)
	if dataDir == "" {
		var err error
		dataDir, err = pasadatos.DefaultDesktopDataDir()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(dataDir, "pasaDATOS.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	logger := log.New(io.MultiWriter(logFile, os.Stdout), "[pasaDATOS] ", log.LstdFlags|log.LUTC)
	configPath := filepath.Join(dataDir, "config.json")
	cfg, err := pasadatos.LoadDesktopConfig(configPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(listenFlag) != "" && listenFlag != cfg.ListenAddress {
		cfg.ListenAddress = listenFlag
		if err := pasadatos.SaveDesktopConfig(configPath, cfg); err != nil {
			return err
		}
	}
	localURL := "http://127.0.0.1:" + portOf(cfg.ListenAddress)
	desktopURL := localURL + "/desktop/?native=" + cfg.NativeToken + "&appv=" + pasadatos.AppVersion
	if info, running := runningDesktopInstance(localURL); running {
		if info.Version == pasadatos.AppVersion {
			if !suppressWindow {
				return pasadatos.OpenDesktopWindow(desktopURL)
			}
			return nil
		}

		logger.Printf("detectada instancia anterior %s; solicitando cierre para actualizar a %s", info.Version, pasadatos.AppVersion)
		if err := stopExistingInstance(localURL, cfg.NativeToken); err != nil {
			return fmt.Errorf("hay una versión anterior de pasaDATOS ejecutándose (%s). Cerrala desde el botón Salir de pasaDATOS y volvé a abrir esta versión: %w", info.Version, err)
		}
		if !waitForInstanceStop(localURL, 60, 100*time.Millisecond) {
			return fmt.Errorf("la versión anterior de pasaDATOS (%s) no terminó de cerrarse. Cerrala desde el Administrador de tareas y volvé a intentar", info.Version)
		}
		logger.Printf("instancia anterior cerrada; iniciando pasaDATOS %s", pasadatos.AppVersion)
	}

	exitCh := make(chan struct{}, 1)
	var agent *pasadatos.DesktopAgent
	server, err := pasadatos.NewHubServer(pasadatos.ServerOptions{
		ListenAddress:   cfg.ListenAddress,
		DataDir:         filepath.Join(dataDir, "local-hub"),
		PublicURL:       "",
		FileTTL:         time.Duration(cfg.LocalFileTTLHour) * time.Hour,
		MetadataTTL:     30 * 24 * time.Hour,
		DesktopMode:     true,
		NativeToken:     cfg.NativeToken,
		DesktopDeviceID: cfg.DeviceID,
		DesktopReceiveCallback: func(transferID string) {
			if agent != nil {
				agent.ReceiveLocalTransfer(transferID)
			}
		},
		DesktopExitCallback: func() {
			select {
			case exitCh <- struct{}{}:
			default:
			}
		},
		Logger: logger,
	})
	if err != nil {
		return err
	}
	agent, err = pasadatos.NewDesktopAgent(dataDir, configPath, server.Store(), logger)
	if err != nil {
		return err
	}
	server.AttachDesktop(agent)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent.Start(ctx)
	defer agent.Stop()
	if cfg.AutoStart {
		_ = pasadatos.SetNativeAutoStart(true)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	if !suppressWindow {
		go func() {
			if waitForHealth(localURL, 40, 100*time.Millisecond) {
				if err := pasadatos.OpenDesktopWindow(desktopURL); err != nil {
					logger.Printf("no se pudo abrir la ventana: %v", err)
				}
			}
		}()
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-sigCtx.Done():
	case <-exitCh:
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	cancel()
	return server.Shutdown()
}

type desktopInstanceInfo struct {
	Service string `json:"service"`
	Version string `json:"version"`
	Desktop bool   `json:"desktop_mode"`
}

func runningDesktopInstance(base string) (desktopInstanceInfo, bool) {
	client := &http.Client{Timeout: 350 * time.Millisecond}
	resp, err := client.Get(base + "/api/v1/health")
	if err != nil {
		return desktopInstanceInfo{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return desktopInstanceInfo{}, false
	}
	var payload desktopInstanceInfo
	if json.NewDecoder(resp.Body).Decode(&payload) != nil {
		return desktopInstanceInfo{}, false
	}
	return payload, payload.Service == pasadatos.AppName && payload.Desktop
}

func anotherInstanceIsRunning(base string) bool {
	_, running := runningDesktopInstance(base)
	return running
}

func stopExistingInstance(base, nativeToken string) error {
	req, err := http.NewRequest(http.MethodPost, base+"/native/exit", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-pasaDATOS-Native", nativeToken)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("el proceso anterior respondió HTTP %d", resp.StatusCode)
	}
	return nil
}

func waitForInstanceStop(base string, attempts int, pause time.Duration) bool {
	for i := 0; i < attempts; i++ {
		if !anotherInstanceIsRunning(base) {
			return true
		}
		time.Sleep(pause)
	}
	return false
}

func waitForHealth(base string, attempts int, pause time.Duration) bool {
	for i := 0; i < attempts; i++ {
		if anotherInstanceIsRunning(base) {
			return true
		}
		time.Sleep(pause)
	}
	return false
}

func portOf(address string) string {
	parts := strings.Split(address, ":")
	if len(parts) > 1 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1]
	}
	return "8765"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func envDurationHours(name string, fallback int64) time.Duration {
	value := envInt64(name, fallback)
	if value <= 0 {
		value = fallback
	}
	return time.Duration(value) * time.Hour
}

func envInt64(name string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

var _ = errors.Is
