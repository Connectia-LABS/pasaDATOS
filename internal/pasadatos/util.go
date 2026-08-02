package pasadatos

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"
)

var unsafeFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)

func randomToken(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func randomID(prefix string) string {
	return prefix + "_" + randomToken(16)
}

func tokenHash(token string) string {
	s := sha256.Sum256([]byte(token))
	return hex.EncodeToString(s[:])
}

func secureTokenEqual(hashHex, token string) bool {
	actual, err := hex.DecodeString(hashHex)
	if err != nil {
		return false
	}
	s := sha256.Sum256([]byte(token))
	if len(actual) != len(s) {
		return false
	}
	return subtle.ConstantTimeCompare(actual, s[:]) == 1
}

func pairingCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 10)
	raw := make([]byte, 10)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		panic(err)
	}
	for i := range raw {
		buf[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(buf[:5]) + "-" + string(buf[5:])
}

func normalizePairingCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, " ", "")
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, "0", "O")
	code = strings.ReplaceAll(code, "1", "I")
	if len(code) == 10 {
		return code[:5] + "-" + code[5:]
	}
	return code
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = unsafeFilenameChars.ReplaceAllString(name, "_")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" || name == "." || name == ".." {
		name = "archivo"
	}
	if len([]rune(name)) > 220 {
		r := []rune(name)
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		maxBase := 200 - len([]rune(ext))
		if maxBase < 20 {
			maxBase = 20
		}
		br := []rune(base)
		if len(br) > maxBase {
			br = br[:maxBase]
		}
		name = string(br) + ext
		_ = r
	}
	return name
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o700)
}

func writeJSONAtomic(path string, value any) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, value any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, value)
}

func desktopDataDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("PASADATOS_DATA_DIR")); v != "" {
		return filepath.Clean(v), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "pasaDATOS"), nil
}

func defaultReceiveFolder() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", "pasaDATOS Recibidos")
	}
	downloads := filepath.Join(home, "Downloads")
	if info, err := os.Stat(downloads); err == nil && info.IsDir() {
		return filepath.Join(downloads, "pasaDATOS")
	}
	descargas := filepath.Join(home, "Descargas")
	if info, err := os.Stat(descargas); err == nil && info.IsDir() {
		return filepath.Join(descargas, "pasaDATOS")
	}
	return filepath.Join(home, "pasaDATOS Recibidos")
}

func defaultDeviceName() string {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "Mi PC"
	}
	return host
}

func normalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("el servidor debe usar http o https")
	}
	if u.Host == "" {
		return "", errors.New("URL de servidor inválida")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func localIPv4s() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			v4 := ip.To4()
			// Exclude APIPA and common virtual networks where possible.
			if v4[0] == 169 && v4[1] == 254 {
				continue
			}
			ips = append(ips, v4.String())
		}
	}
	sort.SliceStable(ips, func(i, j int) bool {
		score := func(ip string) int {
			if strings.HasPrefix(ip, "192.168.") {
				return 0
			}
			if strings.HasPrefix(ip, "10.") {
				return 1
			}
			if strings.HasPrefix(ip, "172.") {
				return 2
			}
			return 3
		}
		return score(ips[i]) < score(ips[j])
	})
	return uniqueStrings(ips)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func contentDisposition(filename string) string {
	clean := strings.ReplaceAll(sanitizeFilename(filename), `"`, "'")
	encoded := url.PathEscape(clean)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	return fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", clean, encoded)
}

func collisionSafePath(folder, filename string) string {
	filename = sanitizeFilename(filename)
	candidate := filepath.Join(folder, filename)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	for i := 1; i < 10000; i++ {
		candidate = filepath.Join(folder, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return filepath.Join(folder, fmt.Sprintf("%s-%d%s", base, time.Now().UnixNano(), ext))
}

func copyFileWithProgress(dst string, src io.Reader, total int64, progress func(int64)) (int64, string, error) {
	if err := ensureDir(filepath.Dir(dst)); err != nil {
		return 0, "", err
	}
	part := dst + ".pasadatos.part"
	file, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, "", err
	}
	h := sha256.New()
	writer := io.MultiWriter(file, h)
	buf := make([]byte, 1024*1024)
	var written int64
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			wn, writeErr := writer.Write(buf[:n])
			written += int64(wn)
			if progress != nil {
				progress(written)
			}
			if writeErr != nil {
				_ = file.Close()
				_ = os.Remove(part)
				return written, "", writeErr
			}
			if wn != n {
				_ = file.Close()
				_ = os.Remove(part)
				return written, "", io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = file.Close()
			_ = os.Remove(part)
			return written, "", readErr
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(part)
		return written, "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(part)
		return written, "", err
	}
	if total > 0 && written != total {
		_ = os.Remove(part)
		return written, "", fmt.Errorf("tamaño recibido inesperado: %d de %d bytes", written, total)
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(dst)
	}
	if err := os.Rename(part, dst); err != nil {
		_ = os.Remove(part)
		return written, "", err
	}
	return written, hex.EncodeToString(h.Sum(nil)), nil
}

// DefaultDesktopDataDir returns the per-user folder used by the Windows app.
func DefaultDesktopDataDir() (string, error) { return desktopDataDir() }
