package core

import (
	"archive/zip"
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// GsVersion — версия встраиваемого портативного Ghostscript.
const GsVersion = "10.05.1"

//go:embed bundle/*
var bundleFS embed.FS

// InstallRoot возвращает путь для распаковки встроенного Ghostscript.
func InstallRoot() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if d, err := os.UserCacheDir(); err == nil {
			base = d
		} else {
			base = os.TempDir()
		}
	}
	return filepath.Join(base, "PdfToolkit", "gs-"+GsVersion)
}

// BundledExePath возвращает ожидаемый путь к распакованному gswin64c.exe.
func BundledExePath() string {
	root := InstallRoot()
	exe := filepath.Join(root, "bin", "gswin64c.exe")
	dll := filepath.Join(root, "bin", "gsdll64.dll")
	marker := filepath.Join(root, ".complete_v2")
	if _, err := os.Stat(exe); err == nil {
		if _, err2 := os.Stat(dll); err2 == nil {
			if _, err3 := os.Stat(marker); err3 == nil {
				return exe
			}
		}
	}
	return ""
}

// EnsureExtracted распаковывает только необходимые файлы Ghostscript (gswin64c.exe, gsdll64.dll, COPYING.txt).
// Все ресурсы Ghostscript 10.05.1 (%rom%Resource, %rom%iccprofiles) уже встроены внутри gsdll64.dll,
// поэтому распаковка 3 файлов вместо 535 занимает доли секунды и не блокируется антивирусом.
func EnsureExtracted() (string, error) {
	if existing := BundledExePath(); existing != "" {
		return existing, nil
	}

	data, err := fs.ReadFile(bundleFS, "bundle/gs.zip")
	if err != nil || len(data) == 0 {
		return "", nil
	}

	root := InstallRoot()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", err
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}

	needed := map[string]bool{
		"bin/gswin64c.exe": true,
		"bin/gsdll64.dll":  true,
		"copying.txt":      true,
	}

	for _, f := range zr.File {
		norm := strings.ToLower(strings.ReplaceAll(f.Name, `\`, `/`))
		if !needed[norm] {
			continue
		}
		dest := filepath.Join(root, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		tmpFile := dest + ".tmp"
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			_ = rc.Close()
			return "", err
		}
		_, copyErr := io.Copy(out, rc)
		_ = out.Close()
		_ = rc.Close()
		if copyErr != nil {
			_ = os.Remove(tmpFile)
			return "", copyErr
		}
		_ = os.Remove(dest)
		if err := os.Rename(tmpFile, dest); err != nil {
			return "", err
		}
	}

	gsExe := filepath.Join(root, "bin", "gswin64c.exe")
	if _, err := os.Stat(gsExe); err != nil {
		return "", fmt.Errorf("в архиве gs.zip не найден bin\\gswin64c.exe")
	}

	marker := filepath.Join(root, ".complete_v2")
	_ = os.WriteFile(marker, []byte(GsVersion), 0o644)
	return gsExe, nil
}

// VcRuntimeInstalled проверяет наличие системных библиотек Visual C++ Redistributable (x64).
func VcRuntimeInstalled() bool {
	if runtime.GOOS != "windows" {
		return true
	}
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = `C:\Windows`
	}
	sys32 := filepath.Join(sysRoot, "System32")
	for _, dll := range []string{"vcruntime140.dll", "msvcp140.dll"} {
		if _, err := os.Stat(filepath.Join(sys32, dll)); err != nil {
			return false
		}
	}
	return true
}
