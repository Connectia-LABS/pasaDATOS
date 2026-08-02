package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	appName    = "pasaDATOS"
	appVersion = "1.1.1"
)

//go:embed payload/*
var payload embed.FS

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "El instalador de pasaDATOS está diseñado para Windows.")
		os.Exit(1)
	}
	uninstall := hasArg("--uninstall")
	elevatedArg := hasArg("--elevated")
	if !elevatedArg && !platformIsElevated() {
		args := []string{"--elevated"}
		if uninstall {
			args = append(args, "--uninstall")
		}
		if err := platformElevate(args); err != nil {
			platformMessage("pasaDATOS", "La instalación necesita autorización de Windows para configurar la red local.\n\n"+err.Error(), true)
			os.Exit(1)
		}
		return
	}
	var err error
	if uninstall {
		err = uninstallApp()
	} else {
		err = installApp()
	}
	if err != nil {
		platformMessage("pasaDATOS", "No se pudo completar la operación.\n\n"+err.Error(), true)
		os.Exit(1)
	}
}

func hasArg(value string) bool {
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(arg, value) {
			return true
		}
	}
	return false
}

func installPaths() (dir, exe, icon, uninstaller string, err error) {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		base, err = os.UserConfigDir()
		if err != nil {
			return "", "", "", "", err
		}
	}
	dir = filepath.Join(base, appName)
	exe = filepath.Join(dir, appName+".exe")
	icon = filepath.Join(dir, appName+".ico")
	uninstaller = filepath.Join(dir, "Desinstalar "+appName+".exe")
	return
}

func installApp() error {
	dir, exe, icon, uninstaller, err := installPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = platformStopApp()
	if err := writeEmbedded("payload/pasaDATOS.exe", exe, 0o755); err != nil {
		return fmt.Errorf("instalar la aplicación: %w", err)
	}
	if err := writeEmbedded("payload/pasaDATOS.ico", icon, 0o644); err != nil {
		return fmt.Errorf("instalar el icono: %w", err)
	}
	if err := writeEmbedded("payload/LEEME-PRIMERO.txt", filepath.Join(dir, "LEEME-PRIMERO.txt"), 0o644); err != nil {
		return fmt.Errorf("instalar la guía: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !samePath(self, uninstaller) {
		if err := copyFile(self, uninstaller); err != nil {
			return fmt.Errorf("crear desinstalador: %w", err)
		}
	}
	if err := platformCreateShortcuts(exe, icon, dir); err != nil {
		return fmt.Errorf("crear accesos directos: %w", err)
	}
	if err := platformRegisterInstall(exe, uninstaller, icon, dir, appVersion); err != nil {
		return fmt.Errorf("registrar instalación: %w", err)
	}
	// The firewall rule is limited to Private network profiles and this exact executable.
	if err := platformAddFirewallRule(exe); err != nil {
		return fmt.Errorf("habilitar recepción en la red privada: %w", err)
	}
	if err := platformLaunch(exe); err != nil {
		return fmt.Errorf("iniciar la aplicación: %w", err)
	}
	platformMessage("pasaDATOS", "¡Instalación completa!\n\npasaDATOS ya está abierto y listo para vincular tu celular.\n\nLos archivos recibidos se guardarán en Descargas\\pasaDATOS.", false)
	return nil
}

func uninstallApp() error {
	dir, _, _, _, err := installPaths()
	if err != nil {
		return err
	}
	_ = platformStopApp()
	_ = platformRemoveFirewallRule()
	_ = platformRemoveRegistration()
	_ = platformRemoveShortcuts()
	dataDir := filepath.Join(strings.TrimSpace(os.Getenv("APPDATA")), appName)
	platformMessage("pasaDATOS", "pasaDATOS se desinstaló correctamente.\n\nLos archivos que recibiste en Descargas no se borraron.", false)
	return platformScheduleRemoval([]string{dir, dataDir})
}

func writeEmbedded(name, destination string, mode fs.FileMode) error {
	data, err := payload.ReadFile(name)
	if err != nil {
		return err
	}
	return writeAtomic(destination, data, mode)
}

func writeAtomic(destination string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	tmp := destination + ".new"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	_ = os.Remove(destination)
	for attempt := 0; attempt < 10; attempt++ {
		if err := os.Rename(tmp, destination); err == nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return os.Rename(tmp, destination)
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writeAtomic(destination, data, 0o755)
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

var _ = errors.Is
