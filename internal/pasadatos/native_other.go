//go:build !windows

package pasadatos

import (
	"errors"
	"os/exec"
	"runtime"
)

func nativePickFiles() ([]string, error) {
	return nil, errors.New("el selector nativo de archivos está disponible en la app para Windows")
}

func nativePickFolder() (string, error) {
	return "", errors.New("el selector nativo de carpetas está disponible en la app para Windows")
}

func nativeOpenPath(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}

func nativeOpenApp(rawURL string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", rawURL).Start()
	}
	return exec.Command("xdg-open", rawURL).Start()
}

func nativeSetAutoStart(enabled bool) error {
	_ = enabled
	return nil
}
