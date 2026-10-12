package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func makeNoisyPdf(t *testing.T, gsPath, dir, pdfOut string, pages, seed int) {
	t.Helper()
	psPath := pdfOut + ".ps"
	ps := fmt.Sprintf(`%%!PS
%d srand
/W 1500 def /H 2000 def
/row W 3 mul string def
1 1 %d {
  pop
  gsave 0 0 translate 612 792 scale
  W H 8 [W 0 0 H neg 0 H]
  { 0 1 row length 1 sub { row exch rand 255 mod put } for row }
  false 3 colorimage
  grestore
  0 setgray /Helvetica findfont 12 scalefont setfont 40 760 moveto (Test page) show
  showpage
} for
`, seed, pages)
	if err := os.WriteFile(psPath, []byte(ps), 0o644); err != nil {
		t.Fatalf("write ps: %v", err)
	}
	cmd := exec.Command(gsPath, "-q", "-dNOPAUSE", "-dBATCH", "-sDEVICE=pdfwrite", "-sOutputFile="+pdfOut, psPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate test pdf failed: %v (%s)", err, string(out))
	}
}

func TestPageRanges(t *testing.T) {
	got, err := ParseRanges("1-3,5,7-", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []PageRange{{1, 3}, {5, 5}, {7, 10}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	got2, err := ParseRanges("2", 5)
	if err != nil || !reflect.DeepEqual(got2, []PageRange{{2, 2}}) {
		t.Fatalf("got %v, err %v", got2, err)
	}

	for _, bad := range []string{"0-2", "9-12", "3-1", "abc", "", "x-3"} {
		if _, err := ParseRanges(bad, 5); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}

	chunks := Chunks(7, 3)
	if !reflect.DeepEqual(chunks, []PageRange{{1, 3}, {4, 6}, {7, 7}}) {
		t.Fatalf("Chunks(7,3) = %v", chunks)
	}

	each := EachPage(3)
	if len(each) != 3 {
		t.Fatalf("EachPage(3) len = %d", len(each))
	}
}

func TestGhostscriptOperations(t *testing.T) {
	gs := NewGhostscriptService("")
	if gs.GsPath == "" {
		t.Skip("Ghostscript not found, skipping integration tests")
	}
	t.Logf("Using Ghostscript: %s", gs.GsPath)

	work := t.TempDir()
	pdfA := filepath.Join(work, "a.pdf")
	pdfB := filepath.Join(work, "b.pdf")
	makeNoisyPdf(t, gs.GsPath, work, pdfA, 5, 7)
	makeNoisyPdf(t, gs.GsPath, work, pdfB, 3, 99)

	ctx := context.Background()

	// 1. PageCount
	if n, ok := gs.PageCount(ctx, pdfA); !ok || n != 5 {
		t.Fatalf("PageCount(pdfA) = %d, %v; want 5, true", n, ok)
	}
	if n, ok := gs.PageCount(ctx, pdfB); !ok || n != 3 {
		t.Fatalf("PageCount(pdfB) = %d, %v; want 3, true", n, ok)
	}

	broken := filepath.Join(work, "broken.pdf")
	_ = os.WriteFile(broken, []byte("%PDF-1.4 broken garbage"), 0o644)
	if n, ok := gs.PageCount(ctx, broken); ok {
		t.Fatalf("expected broken pdf PageCount to fail, got %d", n)
	}

	// 2. Merge
	merged := filepath.Join(work, "merged.pdf")
	if err := gs.Merge(ctx, []string{pdfA, pdfB}, merged); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	if n, ok := gs.PageCount(ctx, merged); !ok || n != 8 {
		t.Fatalf("merged PageCount = %d, %v; want 8", n, ok)
	}

	// 3. Extract
	ext := filepath.Join(work, "ext.pdf")
	if err := gs.Extract(ctx, pdfA, PageRange{2, 4}, ext); err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if n, ok := gs.PageCount(ctx, ext); !ok || n != 3 {
		t.Fatalf("extracted PageCount = %d, %v; want 3", n, ok)
	}

	// 4. Profiles
	origSt, _ := os.Stat(pdfA)
	origSize := origSt.Size()
	for _, idx := range []int{1, 2, 3, 4} {
		dst := filepath.Join(work, fmt.Sprintf("p%d.pdf", idx))
		sz, err := gs.Compress(ctx, pdfA, dst, Profiles[idx])
		if err != nil || sz <= 0 {
			t.Fatalf("Compress profile %d failed: %v (size=%d)", idx, err, sz)
		}
		if n, ok := gs.PageCount(ctx, dst); !ok || n != 5 {
			t.Fatalf("profile %d PageCount = %d, %v", idx, n, ok)
		}
	}

	// 5. AutoCompress 2x
	autoOut := filepath.Join(work, "auto.pdf")
	autoSize, err := gs.AutoCompress(ctx, pdfA, autoOut, 2.0, t.Logf)
	if err != nil {
		t.Fatalf("AutoCompress failed: %v", err)
	}
	if autoSize <= 0 || float64(autoSize) > float64(origSize)/2.0 {
		t.Fatalf("AutoCompress did not achieve 2x: orig=%d, auto=%d", origSize, autoSize)
	}
	if n, ok := gs.PageCount(ctx, autoOut); !ok || n != 5 {
		t.Fatalf("autoOut PageCount = %d, %v", n, ok)
	}

	// 6. Broken PDF in AutoCompress
	brokenSize, _ := gs.AutoCompress(ctx, broken, filepath.Join(work, "broken_out.pdf"), 2.0, nil)
	if brokenSize != 0 {
		t.Fatalf("expected 0 for broken pdf, got %d", brokenSize)
	}

	// 7. Cancellation
	cancelCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()
	_, err = gs.AutoCompress(cancelCtx, pdfA, filepath.Join(work, "canceled.pdf"), 2.0, nil)
	if err == nil {
		t.Fatalf("expected cancellation error")
	}
}
