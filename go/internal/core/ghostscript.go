package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CompressionProfile описывает профиль сжатия PDF через Ghostscript.
type CompressionProfile struct {
	Name           string
	Description    string
	Dpi            int // 0 = АВТО
	JpegQuality    int
	DownsampleType string
	ExtraArgs      []string
}

// Profiles — список доступных профилей сжатия.
var Profiles = []CompressionProfile{
	{
		Name:           "⚡ АВТО — минимум 2x (рекомендую)",
		Description:    "Сам подбирает профиль от мягкого к агрессивному, пока не достигнет цели.",
		Dpi:            0,
		JpegQuality:    0,
		DownsampleType: "/Bicubic",
	},
	{
		Name:           "🔥 Максимальное (2-10x)",
		Description:    "72 DPI, JPEG 30%. Для архива/email.",
		Dpi:            72,
		JpegQuality:    30,
		DownsampleType: "/Bicubic",
		ExtraArgs:      []string{"-dDetectDuplicateImages=true", "-dCompressFonts=true", "-dSubsetFonts=true", "-dRemoveBookmarks=true"},
	},
	{
		Name:           "📄 Баланс (2-4x)",
		Description:    "150 DPI, JPEG 60%. Для документов.",
		Dpi:            150,
		JpegQuality:    60,
		DownsampleType: "/Bicubic",
		ExtraArgs:      []string{"-dDetectDuplicateImages=true", "-dCompressFonts=true", "-dSubsetFonts=true"},
	},
	{
		Name:           "👁 Качество (1.5-2x)",
		Description:    "300 DPI, JPEG 85%. Почти без потерь.",
		Dpi:            300,
		JpegQuality:    85,
		DownsampleType: "/Bicubic",
		ExtraArgs:      []string{"-dDetectDuplicateImages=true", "-dCompressFonts=true", "-dSubsetFonts=true"},
	},
	{
		Name:           "📱 Только текст (макс. сжатие)",
		Description:    "150 DPI, JPEG 40%, Subsample. Для текстовых сканов.",
		Dpi:            150,
		JpegQuality:    40,
		DownsampleType: "/Subsample",
		ExtraArgs: []string{
			"-dColorImageDownsampleType=/Subsample",
			"-dGrayImageDownsampleType=/Subsample",
			"-dMonoImageDownsampleType=/Subsample",
			"-dDetectDuplicateImages=true",
			"-dCompressFonts=true",
			"-dSubsetFonts=true",
		},
	},
}

// AutoOrder — порядок перебора профилей в АВТО-режиме: Качество → Баланс → Только текст → Максимальное.
var AutoOrder = []int{3, 2, 4, 1}

// GhostscriptService — обёртка над Ghostscript для сжатия, объединения и разделения PDF.
type GhostscriptService struct {
	GsPath string
}

// NewGhostscriptService создаёт сервис с указанным или автоматически найденным Ghostscript.
func NewGhostscriptService(gsPath string) *GhostscriptService {
	if gsPath == "" {
		gsPath = FindGhostscript()
	}
	return &GhostscriptService{GsPath: gsPath}
}

// FindGhostscript ищет Ghostscript в GS_PATH, папке распаковки бандла, Program Files и PATH.
func FindGhostscript() string {
	if env := strings.TrimSpace(os.Getenv("GS_PATH")); env != "" {
		if st, err := os.Stat(env); err == nil && !st.IsDir() {
			return env
		}
	}

	if bundled := BundledExePath(); bundled != "" {
		if st, err := os.Stat(bundled); err == nil && !st.IsDir() {
			return bundled
		}
	}

	roots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		`C:\Program Files`,
		`C:\Program Files (x86)`,
	}
	seen := map[string]bool{}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" || seen[strings.ToLower(root)] {
			continue
		}
		seen[strings.ToLower(root)] = true
		gsDir := filepath.Join(root, "gs")
		entries, err := os.ReadDir(gsDir)
		if err != nil {
			continue
		}
		var candidates []string
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(strings.ToLower(e.Name()), "gs") {
				continue
			}
			for _, binName := range []string{"gswin64c.exe", "gswin32c.exe"} {
				p := filepath.Join(gsDir, e.Name(), "bin", binName)
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					candidates = append(candidates, p)
				}
			}
		}
		if len(candidates) > 0 {
			sort.Slice(candidates, func(i, j int) bool {
				return strings.ToLower(candidates[i]) > strings.ToLower(candidates[j])
			})
			return candidates[0]
		}
	}

	for _, name := range []string{"gswin64c.exe", "gswin32c.exe", "gswin64c", "gs"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func (s *GhostscriptService) run(ctx context.Context, args []string, timeout time.Duration) (int, string, string, error) {
	if s.GsPath == "" {
		return -1, "", "", fmt.Errorf("Ghostscript не найден. Укажите путь к gswin64c.exe")
	}
	if _, err := os.Stat(s.GsPath); err != nil {
		return -1, "", "", fmt.Errorf("Ghostscript не найден: %s", s.GsPath)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, s.GsPath, args...)
	configureCmd(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		return -1, stdout.String(), stderr.String(), ctx.Err()
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return -1, stdout.String(), stderr.String(), fmt.Errorf("Таймаут — файл слишком большой или повреждён")
	}
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			return -1, stdout.String(), stderr.String(), err
		}
	}
	return code, stdout.String(), stderr.String(), nil
}

func shortStr(s string, max int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func escapePs(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `(`, `\(`)
	s = strings.ReplaceAll(s, `)`, `\)`)
	return s
}

var pageCountRegex = regexp.MustCompile(`(?m)^\s*(\d+)\s*$`)

// PageCount возвращает число страниц в PDF (и true) либо (0, false), если файл повреждён.
func (s *GhostscriptService) PageCount(ctx context.Context, pdf string) (int, bool) {
	ps := fmt.Sprintf("(%s) (r) file runpdfbegin pdfpagecount = quit", escapePs(pdf))
	for _, extra := range []string{"--permit-file-read=" + pdf, "-dNOSAFER"} {
		code, stdout, _, err := s.run(ctx, []string{"-q", "-dNODISPLAY", extra, "-c", ps}, 2*time.Minute)
		if err != nil {
			if ctx.Err() != nil {
				return 0, false
			}
			continue
		}
		if code == 0 && !strings.Contains(strings.ToLower(stdout), "error") {
			m := pageCountRegex.FindStringSubmatch(stdout)
			if len(m) == 2 {
				if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
					return n, true
				}
			}
		}
	}
	return 0, false
}

func (s *GhostscriptService) runChecked(ctx context.Context, args []string, output string) error {
	code, stdout, stderr, err := s.run(ctx, args, 30*time.Minute)
	if err != nil {
		return err
	}
	if code != 0 {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = strings.TrimSpace(stdout)
		}
		return fmt.Errorf("Ghostscript вернул код %d: %s", code, shortStr(msg, 300))
	}
	if st, err := os.Stat(output); err != nil || st.IsDir() {
		return fmt.Errorf("Выходной файл не создан")
	}
	return nil
}

// Merge объединяет несколько PDF в один без перекодирования изображений.
func (s *GhostscriptService) Merge(ctx context.Context, inputs []string, output string) error {
	if len(inputs) < 2 {
		return fmt.Errorf("Для объединения нужно минимум 2 файла")
	}
	args := []string{
		"-q", "-dNOPAUSE", "-dBATCH", "-sDEVICE=pdfwrite", "-dCompatibilityLevel=1.4",
		"-dPassThroughJPEGImages=true", "-dPassThroughJPXImages=true", "-dDetectDuplicateImages=true",
		"-sOutputFile=" + output,
	}
	args = append(args, inputs...)
	return s.runChecked(ctx, args, output)
}

// Extract извлекает диапазон страниц r.First..r.Last (включительно) без потери качества.
func (s *GhostscriptService) Extract(ctx context.Context, src string, r PageRange, output string) error {
	args := []string{
		"-q", "-dNOPAUSE", "-dBATCH", "-sDEVICE=pdfwrite", "-dCompatibilityLevel=1.4",
		fmt.Sprintf("-dFirstPage=%d", r.First),
		fmt.Sprintf("-dLastPage=%d", r.Last),
		"-dPassThroughJPEGImages=true", "-dPassThroughJPXImages=true",
		"-sOutputFile=" + output,
		src,
	}
	return s.runChecked(ctx, args, output)
}

// Compress сжимает PDF заданным профилем и возвращает размер выходного файла в байтах.
func (s *GhostscriptService) Compress(ctx context.Context, src, dst string, p CompressionProfile) (int64, error) {
	args := []string{
		"-sDEVICE=pdfwrite", "-dCompatibilityLevel=1.4",
		"-dNOPAUSE", "-dQUIET", "-dBATCH", "-dSAFER",
		fmt.Sprintf("-dColorImageResolution=%d", p.Dpi),
		fmt.Sprintf("-dGrayImageResolution=%d", p.Dpi),
		fmt.Sprintf("-dMonoImageResolution=%d", p.Dpi),
		fmt.Sprintf("-dColorImageDownsampleType=%s", p.DownsampleType),
		fmt.Sprintf("-dGrayImageDownsampleType=%s", p.DownsampleType),
		fmt.Sprintf("-dMonoImageDownsampleType=%s", p.DownsampleType),
		fmt.Sprintf("-dJPEGQ=%d", p.JpegQuality),
		"-dAutoFilterColorImages=true", "-dAutoFilterGrayImages=true",
		"-dColorImageFilter=/DCTEncode", "-dGrayImageFilter=/DCTEncode",
		"-dDownsampleColorImages=true", "-dDownsampleGrayImages=true", "-dDownsampleMonoImages=true",
	}
	args = append(args, p.ExtraArgs...)
	args = append(args, "-sOutputFile="+dst, src)

	if err := s.runChecked(ctx, args, dst); err != nil {
		return 0, err
	}
	st, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// AutoCompress перебирает профили от мягкого к агрессивному до достижения targetRatio.
func (s *GhostscriptService) AutoCompress(ctx context.Context, src, dst string, targetRatio float64, logFn func(string)) (int64, error) {
	if logFn == nil {
		logFn = func(string) {}
	}
	st, err := os.Stat(src)
	if err != nil {
		return 0, err
	}
	originalSize := st.Size()
	if targetRatio < 1.05 {
		targetRatio = 1.05
	}
	targetSize := int64(float64(originalSize) / targetRatio)
	ext := filepath.Ext(dst)
	tmp := strings.TrimSuffix(dst, ext) + ".autotmp.pdf"
	defer func() {
		_ = os.Remove(tmp)
	}()

	var bestSize int64
	var bestProfile *CompressionProfile

	for _, idx := range AutoOrder {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		p := Profiles[idx]
		logFn(fmt.Sprintf("   ⚙ Профиль «%s»...", p.Name))
		size, err := s.Compress(ctx, src, tmp, p)
		if err != nil {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			logFn(fmt.Sprintf("      ⚠ %v", err))
			continue
		}
		if size <= 0 {
			continue
		}
		if bestProfile == nil || size < bestSize {
			bestSize = size
			pCopy := p
			bestProfile = &pCopy
		}
		if size <= targetSize {
			_ = os.Remove(dst)
			if err := os.Rename(tmp, dst); err != nil {
				return 0, err
			}
			logFn(fmt.Sprintf("   ✔ Цель достигнута: %s (x%.1f) — «%s»",
				FormatSize(size), float64(originalSize)/float64(size), p.Name))
			return size, nil
		}
		logFn(fmt.Sprintf("      Мало: x%.1f, агрессивнее...", float64(originalSize)/float64(size)))
	}

	if bestProfile != nil && bestSize < originalSize {
		if _, err := s.Compress(ctx, src, dst, *bestProfile); err != nil {
			return 0, err
		}
		logFn(fmt.Sprintf("   ⚠ Цель x%.1f недостижима, лучшее — «%s» (x%.1f)",
			targetRatio, bestProfile.Name, float64(originalSize)/float64(bestSize)))
		return bestSize, nil
	}
	return 0, nil
}
