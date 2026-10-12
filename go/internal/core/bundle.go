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

//GsVersion — версия встраиваемого портативного Ghostscript.
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
	marker := filepath.Join(root, ".complete")
	if _, err := os.Stat(exe); err == nil {
		if _, err2 := os.Stat(marker); err2 == nil {
			return exe
		}
	}
	return ""
}

// EnsureExtracted распаковывает встроенный bundle/gs.zip (если он был встроен при сборке).
func EnsureExtracted() (string, error) {
	if existing := BundledExePath(); existing != "" {
		return existing, nil
	}

	data, err := fs.ReadFile(bundleFS, "bundle/gs.zip")
	if err != nil || len(data) == 0 {
		return "", nil
	}

	root := InstallRoot()
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return "", err
	}

	tmp := root + ".go.tmp"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}

	tmpClean := filepath.Clean(tmp) + string(os.PathSeparator)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}

	for _, f := range zr.File {
		dest := filepath.Clean(filepath.Join(tmp, filepath.FromSlash(f.Name)))
		if !strings.HasPrefix(strings.ToLower(dest), strings.ToLower(tmpClean)) {
			continue // защита от zip-slip
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			_ = rc.Close()
			return "", err
		}
		_, copyErr := io.Copy(out, rc)
		_ = out.Close()
		_ = rc.Close()
		if copyErr != nil {
			return "", copyErr
		}
	}

	gsExeInTmp := filepath.Join(tmp, "bin", "gswin64c.exe")
	if _, err := os.Stat(gsExeInTmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("в архиве gs.zip не найден bin\\gswin64c.exe")
	}

	_ = os.RemoveAll(root)
	if err := os.Rename(tmp, root); err != nil {
		return "", err
	}
	marker := filepath.Join(root, ".complete")
	_ = os.WriteFile(marker, []byte(GsVersion), 0o644)
	return filepath.Join(root, "bin", "gswin64c.exe"), nil
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
