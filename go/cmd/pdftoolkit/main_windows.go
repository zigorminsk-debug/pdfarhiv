//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"pdftoolkit/internal/core"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")

	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW      = user32.NewProc("RegisterClassExW")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procShowWindow            = user32.NewProc("ShowWindow")
	procUpdateWindow          = user32.NewProc("UpdateWindow")
	procGetMessageW           = user32.NewProc("GetMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procDispatchMessageW      = user32.NewProc("DispatchMessageW")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procSetWindowTextW        = user32.NewProc("SetWindowTextW")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	procEnableWindow          = user32.NewProc("EnableWindow")
	procLoadCursorW           = user32.NewProc("LoadCursorW")
	procLoadIconW             = user32.NewProc("LoadIconW")
	procMessageBoxW           = user32.NewProc("MessageBoxW")
	procGetKeyState           = user32.NewProc("GetKeyState")
	procIsDialogMessageW      = user32.NewProc("IsDialogMessageW")

	procCreateFontW           = gdi32.NewProc("CreateFontW")
	procInitCommonControlsEx  = comctl32.NewProc("InitCommonControlsEx")
	procGetOpenFileNameW      = comdlg32.NewProc("GetOpenFileNameW")
	procGetSaveFileNameW      = comdlg32.NewProc("GetSaveFileNameW")

	procDragAcceptFiles       = shell32.NewProc("DragAcceptFiles")
	procDragQueryFileW        = shell32.NewProc("DragQueryFileW")
	procDragFinish            = shell32.NewProc("DragFinish")
	procSHBrowseForFolderW    = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW  = shell32.NewProc("SHGetPathFromIDListW")
	procShellExecuteW         = shell32.NewProc("ShellExecuteW")

	procCoInitializeEx        = ole32.NewProc("CoInitializeEx")
	procCoTaskMemFree         = ole32.NewProc("CoTaskMemFree")
)

const (
	wsOverlappedWindow = 0x00CF0000
	wsClipChildren     = 0x02000000
	wsVisible          = 0x10000000
	wsChild            = 0x40000000
	wsTabStop          = 0x00010000
	wsBorder           = 0x00800000
	wsVScroll          = 0x00200000
	wsHScroll          = 0x00100000
	wsDisabled         = 0x08000000

	wsExClientEdge     = 0x00000200
	wsExAcceptFiles    = 0x00000010

	swShow             = 5
	swHide             = 0

	wmDestroy          = 0x0002
	wmClose            = 0x0010
	wmSetFont          = 0x0030
	wmNotify           = 0x004E
	wmCommand          = 0x0111
	wmKeyDown          = 0x0100
	wmDropFiles        = 0x0233
	wmAppRunUI         = 0x8001

	bsPushButton       = 0x00000000
	bsAutoRadioButton  = 0x00000009
	bmGetCheck         = 0x00F0
	bmSetCheck         = 0x00F1
	bstChecked         = 1

	cbsDropDownList    = 0x0003
	cbAddString        = 0x0143
	cbSetCurSel        = 0x014E
	cbGetCurSel        = 0x0147
	cbnSelChange       = 1

	lbsNotify          = 0x0001
	lbsExtendedSel     = 0x0800
	lbsNoIntegralHeight = 0x0100
	lbAddString        = 0x0180
	lbResetContent     = 0x0184
	lbGetSelCount      = 0x0190
	lbGetSelItems      = 0x0191
	lbSetSel           = 0x0185
	lbSetHorizontalExtent = 0x0194

	esMultiline        = 0x0004
	esAutoVScroll      = 0x0040
	esAutoHScroll      = 0x0080
	esReadOnly         = 0x0800
	esNumber           = 0x2000
	emSetSel           = 0x00B1
	emReplaceSel       = 0x00C2
	emScrollCaret      = 0x00B7

	pbmSetRange32      = 0x0406
	pbmSetPos          = 0x0402

	tcmInsertItemW     = 0x133E
	tcmGetCurSel       = 0x130B
	tcifText           = 0x0001
	tcnSelChange       = ^uint32(550) // -551

	mbOK               = 0x00000000
	mbYesNo            = 0x00000004
	mbIconInformation  = 0x00000040
	mbIconWarning      = 0x00000030
	mbIconError        = 0x00000010
	mbIconQuestion     = 0x00000020
	idYes              = 6
	idNo               = 7

	ofnExplorer        = 0x00080000
	ofnFileMustExist   = 0x00001000
	ofnPathMustExist   = 0x00000800
	ofnAllowMultiSelect = 0x00000200
	ofnNoChangeDir     = 0x00000008
	ofnOverwritePrompt = 0x00000002

	bifReturnOnlyFsDirs = 0x00000001
	bifNewDialogStyle   = 0x00000040

	vkControl          = 0x11
	vkEscape           = 0x1B
	vkDelete           = 0x2E
	vkF5               = 0x74
)

// Control IDs
const (
	idGsManual = 101 + iota
	idGsDownload
	idTabs
	idStop

	// Tab 0: Compress
	idCompList
	idCompAddFiles
	idCompAddFolder
	idCompRemove
	idCompClear
	idCompProfile
	idCompTarget
	idCompPickOut
	idCompResetOut
	idCompStart

	// Tab 1: Merge
	idMergeList
	idMergeAddFiles
	idMergeAddFolder
	idMergeUp
	idMergeDown
	idMergeRemove
	idMergeClear
	idMergeSaveAs
	idMergeStart

	// Tab 2: Split
	idSplitList
	idSplitAddFiles
	idSplitAddFolder
	idSplitRemove
	idSplitClear
	idSplitRbEach
	idSplitRbChunks
	idSplitRbRanges
	idSplitPickOut
	idSplitResetOut
	idSplitStart
)

type point struct {
	x, y int32
}

type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type initCommonControlsEx struct {
	dwSize uint32
	dwICC  uint32
}

type tcItemW struct {
	mask        uint32
	dwState     uint32
	dwStateMask uint32
	pszText     *uint16
	cchTextMax  int32
	iImage      int32
	lParam      uintptr
}

type nmhdr struct {
	hwndFrom uintptr
	idFrom   uintptr
	code     uint32
}

type openFileNameW struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

type browseInfoW struct {
	hwndOwner      uintptr
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

type App struct {
	hInstance uintptr
	hwnd      uintptr
	fontUI    uintptr
	fontBold  uintptr
	fontMono  uintptr

	gs        *core.GhostscriptService
	busy      bool
	cancelFn  context.CancelFunc

	uiMu      sync.Mutex
	uiQueue   []func()

	// Common controls
	lblGsStatus uintptr
	btnGsManual uintptr
	btnGsDl     uintptr
	tabCtrl     uintptr
	progressBar uintptr
	btnStop     uintptr
	lblStatus   uintptr
	edLog       uintptr

	// Tab groups (for show/hide switching)
	tabControls [3][]uintptr
	actionBtns  []uintptr

	// Compress tab state & controls
	compFiles   []string
	lbComp      uintptr
	cbProfile   uintptr
	cbTarget    uintptr
	lblProfDesc uintptr
	edCompOut   uintptr
	compOutDir  string
	btnCompRun  uintptr

	// Merge tab state & controls
	mergeFiles  []string
	lbMerge     uintptr
	edMergeOut  uintptr
	btnMergeRun uintptr

	// Split tab state & controls
	splitFiles  []string
	lbSplit     uintptr
	rbEach      uintptr
	rbChunks    uintptr
	edChunkSize uintptr
	rbRanges    uintptr
	edRanges    uintptr
	edSplitOut  uintptr
	splitOutDir string
	btnSplitRun uintptr
}

var globalApp *App

func u16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func makeFont(height int32, weight int32, face string) uintptr {
	f, _, _ := procCreateFontW.Call(
		uintptr(uint32(height)), 0, 0, 0,
		uintptr(weight), 0, 0, 0,
		1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(u16(face))),
	)
	return f
}

func (a *App) createCtrl(exStyle uint32, className, text string, style uint32, x, y, w, h int32, id uintptr, font uintptr) uintptr {
	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(u16(className))),
		uintptr(unsafe.Pointer(u16(text))),
		uintptr(style|wsChild),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		a.hwnd,
		id,
		a.hInstance,
		0,
	)
	if font == 0 {
		font = a.fontUI
	}
	procSendMessageW.Call(hwnd, wmSetFont, font, 1)
	return hwnd
}

func (a *App) addTabCtrl(tabIdx int, hwnd uintptr) uintptr {
	a.tabControls[tabIdx] = append(a.tabControls[tabIdx], hwnd)
	return hwnd
}

func setText(hwnd uintptr, s string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(u16(s))))
}

func getText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), length+1)
	return syscall.UTF16ToString(buf)
}

func (a *App) postUI(fn func()) {
	a.uiMu.Lock()
	a.uiQueue = append(a.uiQueue, fn)
	a.uiMu.Unlock()
	procPostMessageW.Call(a.hwnd, wmAppRunUI, 0, 0)
}

func (a *App) drainUI() {
	for {
		a.uiMu.Lock()
		if len(a.uiQueue) == 0 {
			a.uiMu.Unlock()
			return
		}
		batch := a.uiQueue
		a.uiQueue = nil
		a.uiMu.Unlock()
		for _, fn := range batch {
			fn()
		}
	}
}

func (a *App) log(msg string) {
	a.postUI(func() {
		line := msg + "\r\n"
		length, _, _ := procGetWindowTextLengthW.Call(a.edLog)
		procSendMessageW.Call(a.edLog, emSetSel, length, length)
		procSendMessageW.Call(a.edLog, emReplaceSel, 0, uintptr(unsafe.Pointer(u16(line))))
		procSendMessageW.Call(a.edLog, emScrollCaret, 0, 0)
	})
}

func (a *App) setStatus(msg string) {
	a.postUI(func() {
		setText(a.lblStatus, msg)
	})
}

func (a *App) setProgress(val int) {
	if val < 0 {
		val = 0
	} else if val > 100 {
		val = 100
	}
	a.postUI(func() {
		procSendMessageW.Call(a.progressBar, pbmSetPos, uintptr(val), 0)
	})
}

func (a *App) setBusy(busy bool) {
	a.busy = busy
	stopEn := uintptr(0)
	actEn := uintptr(1)
	if busy {
		stopEn = 1
		actEn = 0
	}
	procEnableWindow.Call(a.btnStop, stopEn)
	for _, b := range a.actionBtns {
		procEnableWindow.Call(b, actEn)
	}
}

func (a *App) msgBox(text, title string, flags uint32) int {
	r, _, _ := procMessageBoxW.Call(
		a.hwnd,
		uintptr(unsafe.Pointer(u16(text))),
		uintptr(unsafe.Pointer(u16(title))),
		uintptr(flags),
	)
	return int(r)
}

func (a *App) buildUI() {
	// Top bar
	a.createCtrl(0, "STATIC", "Ghostscript:", wsVisible, 14, 12, 80, 22, 0, a.fontBold)
	a.lblGsStatus = a.createCtrl(0, "STATIC", "⏳ Подготовка встроенного Ghostscript...", wsVisible, 98, 12, 540, 22, 0, a.fontUI)
	a.btnGsManual = a.createCtrl(0, "BUTTON", "🔍 Указать вручную", wsVisible|wsTabStop|bsPushButton, 650, 8, 155, 28, idGsManual, a.fontUI)
	a.btnGsDl = a.createCtrl(0, "BUTTON", "📥 Скачать Ghostscript", wsVisible|wsTabStop|bsPushButton, 812, 8, 160, 28, idGsDownload, a.fontUI)

	// Tab control
	a.tabCtrl = a.createCtrl(0, "SysTabControl32", "", wsVisible|wsTabStop|wsClipChildren, 12, 42, 960, 445, idTabs, a.fontBold)
	for i, title := range []string{" 🗜 Сжать ", " 🔗 Объединить ", " ✂ Разделить "} {
		item := tcItemW{
			mask:    tcifText,
			pszText: u16(title),
		}
		procSendMessageW.Call(a.tabCtrl, tcmInsertItemW, uintptr(i), uintptr(unsafe.Pointer(&item)))
	}

	// ---- TAB 0: СЖАТЬ ----
	a.addTabCtrl(0, a.createCtrl(0, "STATIC", "Файлы для сжатия (перетащите PDF прямо в окно программы):", wsVisible, 26, 78, 600, 20, 0, a.fontUI))
	a.lbComp = a.addTabCtrl(0, a.createCtrl(wsExClientEdge, "LISTBOX", "", wsVisible|wsTabStop|wsVScroll|wsHScroll|lbsNotify|lbsExtendedSel|lbsNoIntegralHeight, 26, 100, 930, 195, idCompList, a.fontMono))
	procSendMessageW.Call(a.lbComp, lbSetHorizontalExtent, 1200, 0)

	btnCompAdd := a.addTabCtrl(0, a.createCtrl(0, "BUTTON", "➕ Добавить файлы", wsVisible|wsTabStop, 26, 302, 150, 28, idCompAddFiles, a.fontUI))
	btnCompDir := a.addTabCtrl(0, a.createCtrl(0, "BUTTON", "📁 Добавить папку", wsVisible|wsTabStop, 182, 302, 150, 28, idCompAddFolder, a.fontUI))
	btnCompDel := a.addTabCtrl(0, a.createCtrl(0, "BUTTON", "🗑 Удалить", wsVisible|wsTabStop, 338, 302, 110, 28, idCompRemove, a.fontUI))
	btnCompClr := a.addTabCtrl(0, a.createCtrl(0, "BUTTON", "🧹 Очистить", wsVisible|wsTabStop, 454, 302, 110, 28, idCompClear, a.fontUI))

	a.addTabCtrl(0, a.createCtrl(0, "STATIC", "Профиль сжатия:", wsVisible, 26, 342, 125, 22, 0, a.fontBold))
	a.cbProfile = a.addTabCtrl(0, a.createCtrl(0, "COMBOBOX", "", wsVisible|wsTabStop|cbsDropDownList, 155, 338, 340, 200, idCompProfile, a.fontUI))
	for _, p := range core.Profiles {
		procSendMessageW.Call(a.cbProfile, cbAddString, 0, uintptr(unsafe.Pointer(u16(p.Name))))
	}
	procSendMessageW.Call(a.cbProfile, cbSetCurSel, 0, 0)

	a.addTabCtrl(0, a.createCtrl(0, "STATIC", "Цель:", wsVisible, 510, 342, 45, 22, 0, a.fontUI))
	a.cbTarget = a.addTabCtrl(0, a.createCtrl(0, "COMBOBOX", "", wsVisible|wsTabStop|cbsDropDownList, 558, 338, 70, 160, idCompTarget, a.fontUI))
	for _, t := range []string{"1.5", "2.0", "3.0", "5.0", "10.0"} {
		procSendMessageW.Call(a.cbTarget, cbAddString, 0, uintptr(unsafe.Pointer(u16(t))))
	}
	procSendMessageW.Call(a.cbTarget, cbSetCurSel, 1, 0)
	a.addTabCtrl(0, a.createCtrl(0, "STATIC", "x", wsVisible, 634, 342, 20, 22, 0, a.fontUI))

	a.lblProfDesc = a.addTabCtrl(0, a.createCtrl(0, "STATIC", core.Profiles[0].Description, wsVisible, 155, 368, 790, 20, 0, a.fontUI))

	a.addTabCtrl(0, a.createCtrl(0, "STATIC", "Папка сохранения:", wsVisible, 26, 398, 125, 22, 0, a.fontUI))
	a.edCompOut = a.addTabCtrl(0, a.createCtrl(wsExClientEdge, "EDIT", "[Рядом с оригиналом, суффикс _compressed]", wsVisible|esReadOnly|esAutoHScroll, 155, 395, 610, 24, 0, a.fontUI))
	btnCompPick := a.addTabCtrl(0, a.createCtrl(0, "BUTTON", "📂 Выбрать", wsVisible|wsTabStop, 772, 393, 95, 28, idCompPickOut, a.fontUI))
	btnCompReset := a.addTabCtrl(0, a.createCtrl(0, "BUTTON", "🔄 Сброс", wsVisible|wsTabStop, 872, 393, 84, 28, idCompResetOut, a.fontUI))

	a.btnCompRun = a.addTabCtrl(0, a.createCtrl(0, "BUTTON", "▶  СТАРТ СЖАТИЯ", wsVisible|wsTabStop, 26, 434, 220, 38, idCompStart, a.fontBold))

	// ---- TAB 1: ОБЪЕДИНИТЬ ----
	a.addTabCtrl(1, a.createCtrl(0, "STATIC", "Порядок файлов = порядок страниц в результате (меняйте кнопками ⬆⬇). Качество изображений сохраняется полностью.", 0, 26, 78, 920, 20, 0, a.fontUI))
	a.lbMerge = a.addTabCtrl(1, a.createCtrl(wsExClientEdge, "LISTBOX", "", wsTabStop|wsVScroll|wsHScroll|lbsNotify|lbsExtendedSel|lbsNoIntegralHeight, 26, 100, 930, 235, idMergeList, a.fontMono))
	procSendMessageW.Call(a.lbMerge, lbSetHorizontalExtent, 1200, 0)

	btnMergeAdd := a.addTabCtrl(1, a.createCtrl(0, "BUTTON", "➕ Добавить файлы", wsTabStop, 26, 342, 150, 28, idMergeAddFiles, a.fontUI))
	btnMergeDir := a.addTabCtrl(1, a.createCtrl(0, "BUTTON", "📁 Добавить папку", wsTabStop, 182, 342, 150, 28, idMergeAddFolder, a.fontUI))
	btnMergeUp := a.addTabCtrl(1, a.createCtrl(0, "BUTTON", "⬆ Вверх", wsTabStop, 338, 342, 95, 28, idMergeUp, a.fontUI))
	btnMergeDn := a.addTabCtrl(1, a.createCtrl(0, "BUTTON", "⬇ Вниз", wsTabStop, 439, 342, 95, 28, idMergeDown, a.fontUI))
	btnMergeDel := a.addTabCtrl(1, a.createCtrl(0, "BUTTON", "🗑 Удалить", wsTabStop, 540, 342, 110, 28, idMergeRemove, a.fontUI))
	btnMergeClr := a.addTabCtrl(1, a.createCtrl(0, "BUTTON", "🧹 Очистить", wsTabStop, 656, 342, 110, 28, idMergeClear, a.fontUI))

	a.addTabCtrl(1, a.createCtrl(0, "STATIC", "Выходной файл:", 0, 26, 392, 120, 22, 0, a.fontBold))
	a.edMergeOut = a.addTabCtrl(1, a.createCtrl(wsExClientEdge, "EDIT", "", wsTabStop|esAutoHScroll, 150, 389, 650, 24, 0, a.fontUI))
	btnMergeSave := a.addTabCtrl(1, a.createCtrl(0, "BUTTON", "📂 Сохранить как...", wsTabStop, 808, 387, 148, 28, idMergeSaveAs, a.fontUI))

	a.btnMergeRun = a.addTabCtrl(1, a.createCtrl(0, "BUTTON", "🔗  ОБЪЕДИНИТЬ", wsTabStop, 26, 434, 220, 38, idMergeStart, a.fontBold))

	// ---- TAB 2: РАЗДЕЛИТЬ ----
	a.addTabCtrl(2, a.createCtrl(0, "STATIC", "Качество не теряется. Результаты сохраняются в подпапку «<имя>_pages» рядом с файлом.", 0, 26, 78, 920, 20, 0, a.fontUI))
	a.lbSplit = a.addTabCtrl(2, a.createCtrl(wsExClientEdge, "LISTBOX", "", wsTabStop|wsVScroll|wsHScroll|lbsNotify|lbsExtendedSel|lbsNoIntegralHeight, 26, 100, 930, 195, idSplitList, a.fontMono))
	procSendMessageW.Call(a.lbSplit, lbSetHorizontalExtent, 1200, 0)

	btnSplitAdd := a.addTabCtrl(2, a.createCtrl(0, "BUTTON", "➕ Добавить файлы", wsTabStop, 26, 302, 150, 28, idSplitAddFiles, a.fontUI))
	btnSplitDir := a.addTabCtrl(2, a.createCtrl(0, "BUTTON", "📁 Добавить папку", wsTabStop, 182, 302, 150, 28, idSplitAddFolder, a.fontUI))
	btnSplitDel := a.addTabCtrl(2, a.createCtrl(0, "BUTTON", "🗑 Удалить", wsTabStop, 338, 302, 110, 28, idSplitRemove, a.fontUI))
	btnSplitClr := a.addTabCtrl(2, a.createCtrl(0, "BUTTON", "🧹 Очистить", wsTabStop, 454, 302, 110, 28, idSplitClear, a.fontUI))

	a.rbEach = a.addTabCtrl(2, a.createCtrl(0, "BUTTON", "Каждая страница — отдельный PDF", wsTabStop|bsAutoRadioButton, 26, 340, 260, 24, idSplitRbEach, a.fontUI))
	procSendMessageW.Call(a.rbEach, bmSetCheck, bstChecked, 0)

	a.rbChunks = a.addTabCtrl(2, a.createCtrl(0, "BUTTON", "Порциями по:", wsTabStop|bsAutoRadioButton, 300, 340, 115, 24, idSplitRbChunks, a.fontUI))
	a.edChunkSize = a.addTabCtrl(2, a.createCtrl(wsExClientEdge, "EDIT", "2", wsTabStop|esNumber, 418, 340, 55, 24, 0, a.fontUI))
	a.addTabCtrl(2, a.createCtrl(0, "STATIC", "стр.", 0, 478, 344, 35, 20, 0, a.fontUI))

	a.rbRanges = a.addTabCtrl(2, a.createCtrl(0, "BUTTON", "Диапазоны:", wsTabStop|bsAutoRadioButton, 530, 340, 105, 24, idSplitRbRanges, a.fontUI))
	a.edRanges = a.addTabCtrl(2, a.createCtrl(wsExClientEdge, "EDIT", "1-3,5,7-10", wsTabStop|esAutoHScroll, 638, 340, 180, 24, 0, a.fontUI))
	a.addTabCtrl(2, a.createCtrl(0, "STATIC", "(напр. 1-3,5,7-)", 0, 824, 344, 130, 20, 0, a.fontUI))

	a.addTabCtrl(2, a.createCtrl(0, "STATIC", "Папка сохранения:", 0, 26, 395, 125, 22, 0, a.fontUI))
	a.edSplitOut = a.addTabCtrl(2, a.createCtrl(wsExClientEdge, "EDIT", "[Подпапка «<имя>_pages» рядом с файлом]", esReadOnly|esAutoHScroll, 155, 392, 610, 24, 0, a.fontUI))
	btnSplitPick := a.addTabCtrl(2, a.createCtrl(0, "BUTTON", "📂 Выбрать", wsTabStop, 772, 390, 95, 28, idSplitPickOut, a.fontUI))
	btnSplitReset := a.addTabCtrl(2, a.createCtrl(0, "BUTTON", "🔄 Сброс", wsTabStop, 872, 390, 84, 28, idSplitResetOut, a.fontUI))

	a.btnSplitRun = a.addTabCtrl(2, a.createCtrl(0, "BUTTON", "✂  РАЗДЕЛИТЬ", wsTabStop, 26, 434, 220, 38, idSplitStart, a.fontBold))

	// ---- BOTTOM BAR ----
	a.progressBar = a.createCtrl(0, "msctls_progress32", "", wsVisible, 12, 496, 865, 24, 0, a.fontUI)
	procSendMessageW.Call(a.progressBar, pbmSetRange32, 0, 100)
	a.btnStop = a.createCtrl(0, "BUTTON", "⏹ Стоп", wsVisible|wsDisabled|wsTabStop, 884, 494, 88, 28, idStop, a.fontUI)

	a.lblStatus = a.createCtrl(0, "STATIC", "Готов. Добавьте PDF и выберите операцию.", wsVisible, 12, 526, 960, 20, 0, a.fontUI)

	a.createCtrl(0, "STATIC", "Лог:", wsVisible, 12, 548, 100, 18, 0, a.fontBold)
	a.edLog = a.createCtrl(wsExClientEdge, "EDIT", "", wsVisible|wsVScroll|esMultiline|esAutoVScroll|esReadOnly, 12, 568, 960, 145, 0, a.fontMono)

	a.actionBtns = []uintptr{
		a.btnCompRun, a.btnMergeRun, a.btnSplitRun,
		btnCompAdd, btnCompDir, btnCompDel, btnCompClr, btnCompPick, btnCompReset,
		btnMergeAdd, btnMergeDir, btnMergeUp, btnMergeDn, btnMergeDel, btnMergeClr, btnMergeSave,
		btnSplitAdd, btnSplitDir, btnSplitDel, btnSplitClr, btnSplitPick, btnSplitReset,
	}
}

func (a *App) currentTab() int {
	idx, _, _ := procSendMessageW.Call(a.tabCtrl, tcmGetCurSel, 0, 0)
	if idx > 2 {
		return 0
	}
	return int(idx)
}

func (a *App) switchTab(tab int) {
	for t := 0; t < 3; t++ {
		cmd := uintptr(swHide)
		if t == tab {
			cmd = uintptr(swShow)
		}
		for _, ctrl := range a.tabControls[t] {
			procShowWindow.Call(ctrl, cmd)
		}
	}
}

func (a *App) initBundledAsync() {
	go func() {
		a.postUI(func() {
			a.setBusy(true)
			setText(a.lblStatus, "Подготовка встроенного Ghostscript (первый запуск — несколько секунд)...")
		})
		bundled, err := core.EnsureExtracted()
		if err != nil {
			a.log("⚠ Не удалось распаковать встроенный Ghostscript: " + err.Error())
		} else if bundled != "" {
			a.gs.GsPath = bundled
			a.log("Встроенный Ghostscript готов: " + bundled)
		}
		if a.gs.GsPath == "" {
			a.gs.GsPath = core.FindGhostscript()
		}
		a.postUI(func() {
			a.setBusy(false)
			a.checkGhostscript()
			setText(a.lblStatus, "Готов. Добавьте PDF и выберите операцию.")
		})
	}()
}

func (a *App) checkGhostscript() {
	if a.gs.GsPath != "" {
		if st, err := os.Stat(a.gs.GsPath); err == nil && !st.IsDir() {
			setText(a.lblGsStatus, "✅ Найден: "+a.gs.GsPath)
			a.log("Ghostscript найден: " + a.gs.GsPath)
			return
		}
	}
	setText(a.lblGsStatus, "❌ НЕ НАЙДЕН — установите Ghostscript или укажите путь")
	a.log("⚠ Ghostscript не найден. Скачайте: https://ghostscript.com/releases/gsdnld.html")
}

func (a *App) requireGs() bool {
	if a.gs.GsPath != "" {
		if st, err := os.Stat(a.gs.GsPath); err == nil && !st.IsDir() {
			return true
		}
	}
	a.msgBox("Ghostscript не найден!\nУстановите его: https://ghostscript.com/releases/gsdnld.html", "Ошибка", mbOK|mbIconError)
	return false
}

// ---- ФАЙЛОВЫЕ СПИСКИ ----

func (a *App) filesPtr(tab int) (*[]string, uintptr, bool) {
	switch tab {
	case 0:
		return &a.compFiles, a.lbComp, false
	case 1:
		return &a.mergeFiles, a.lbMerge, true
	default:
		return &a.splitFiles, a.lbSplit, false
	}
}

func (a *App) refreshList(tab int) {
	files, lb, numbered := a.filesPtr(tab)
	procSendMessageW.Call(lb, lbResetContent, 0, 0)
	for i, f := range *files {
		var size int64
		if st, err := os.Stat(f); err == nil {
			size = st.Size()
		}
		prefix := ""
		if numbered {
			prefix = fmt.Sprintf("%d. ", i+1)
		}
		line := fmt.Sprintf("%s%s   [%s]   —   %s", prefix, filepath.Base(f), core.FormatSize(size), filepath.Dir(f))
		procSendMessageW.Call(lb, lbAddString, 0, uintptr(unsafe.Pointer(u16(line))))
	}
}

func tryAddFile(list *[]string, p string) {
	if !strings.EqualFold(filepath.Ext(p), ".pdf") {
		return
	}
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return
	}
	full, err := filepath.Abs(p)
	if err != nil {
		full = p
	}
	for _, existing := range *list {
		if strings.EqualFold(existing, full) {
			return
		}
	}
	*list = append(*list, full)
}

func addFolderRecursive(list *[]string, folder string) {
	var found []string
	_ = filepath.WalkDir(folder, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ".pdf") {
			found = append(found, p)
		}
		return nil
	})
	sort.Slice(found, func(i, j int) bool {
		return strings.ToLower(found[i]) < strings.ToLower(found[j])
	})
	for _, f := range found {
		tryAddFile(list, f)
	}
}

func getSelectedIndices(lb uintptr) []int {
	count, _, _ := procSendMessageW.Call(lb, lbGetSelCount, 0, 0)
	if int32(count) <= 0 {
		return nil
	}
	buf := make([]int32, count)
	procSendMessageW.Call(lb, lbGetSelItems, count, uintptr(unsafe.Pointer(&buf[0])))
	res := make([]int, len(buf))
	for i, v := range buf {
		res[i] = int(v)
	}
	return res
}

func (a *App) removeSelected(tab int) {
	files, lb, _ := a.filesPtr(tab)
	sel := getSelectedIndices(lb)
	if len(sel) == 0 {
		return
	}
	for i := len(sel) - 1; i >= 0; i-- {
		idx := sel[i]
		if idx >= 0 && idx < len(*files) {
			*files = append((*files)[:idx], (*files)[idx+1:]...)
		}
	}
	a.refreshList(tab)
}

func (a *App) moveMergeSelected(delta int) {
	sel := getSelectedIndices(a.lbMerge)
	if len(sel) == 0 {
		return
	}
	if delta < 0 && sel[0] == 0 {
		return
	}
	if delta > 0 && sel[len(sel)-1] >= len(a.mergeFiles)-1 {
		return
	}
	if delta < 0 {
		for _, i := range sel {
			j := i + delta
			a.mergeFiles[i], a.mergeFiles[j] = a.mergeFiles[j], a.mergeFiles[i]
		}
	} else {
		for k := len(sel) - 1; k >= 0; k-- {
			i := sel[k]
			j := i + delta
			a.mergeFiles[i], a.mergeFiles[j] = a.mergeFiles[j], a.mergeFiles[i]
		}
	}
	a.refreshList(1)
	for _, i := range sel {
		procSendMessageW.Call(a.lbMerge, lbSetSel, 1, uintptr(i+delta))
	}
}

// ---- ДИАЛОГИ ----

func makeFilter(parts ...string) *uint16 {
	var u []uint16
	for _, p := range parts {
		s, _ := syscall.UTF16FromString(p)
		u = append(u, s...)
	}
	u = append(u, 0)
	return &u[0]
}

func (a *App) openPdfDialog(tab int) {
	buf := make([]uint16, 32768)
	ofn := openFileNameW{
		lStructSize: uint32(unsafe.Sizeof(openFileNameW{})),
		hwndOwner:   a.hwnd,
		lpstrFilter: makeFilter("PDF файлы (*.pdf)", "*.pdf", "Все файлы (*.*)", "*.*"),
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  u16("Выберите PDF файлы"),
		flags:       ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnAllowMultiSelect | ofnNoChangeDir,
	}
	ok, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ok == 0 {
		return
	}
	files, _, _ := a.filesPtr(tab)
	// Разбираем мультивыбор: либо один полный путь, либо папка\0файл1\0файл2\0\0
	var tokens []string
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] == 0 {
			if i == start {
				break
			}
			tokens = append(tokens, syscall.UTF16ToString(buf[start:i]))
			start = i + 1
		}
	}
	if len(tokens) == 1 {
		tryAddFile(files, tokens[0])
	} else if len(tokens) > 1 {
		dir := tokens[0]
		for _, name := range tokens[1:] {
			tryAddFile(files, filepath.Join(dir, name))
		}
	}
	a.refreshList(tab)
}

func (a *App) selectGsDialog() {
	buf := make([]uint16, 1024)
	ofn := openFileNameW{
		lStructSize: uint32(unsafe.Sizeof(openFileNameW{})),
		hwndOwner:   a.hwnd,
		lpstrFilter: makeFilter("Ghostscript (gswin*c.exe)", "gswin*c.exe", "Все файлы (*.*)", "*.*"),
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  u16("Выберите gswin64c.exe"),
		flags:       ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnNoChangeDir,
	}
	ok, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ok == 0 {
		return
	}
	a.gs.GsPath = syscall.UTF16ToString(buf)
	a.checkGhostscript()
}

func (a *App) savePdfDialog() string {
	buf := make([]uint16, 1024)
	defName, _ := syscall.UTF16FromString("merged.pdf")
	copy(buf, defName)
	ofn := openFileNameW{
		lStructSize: uint32(unsafe.Sizeof(openFileNameW{})),
		hwndOwner:   a.hwnd,
		lpstrFilter: makeFilter("PDF файлы (*.pdf)", "*.pdf"),
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  u16("Сохранить объединённый PDF как"),
		lpstrDefExt: u16("pdf"),
		flags:       ofnExplorer | ofnPathMustExist | ofnOverwritePrompt | ofnNoChangeDir,
	}
	ok, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func (a *App) browseFolderDialog(title string) string {
	disp := make([]uint16, 260)
	bi := browseInfoW{
		hwndOwner:      a.hwnd,
		pszDisplayName: &disp[0],
		lpszTitle:      u16(title),
		ulFlags:        bifReturnOnlyFsDirs | bifNewDialogStyle,
	}
	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer procCoTaskMemFree.Call(pidl)
	pathBuf := make([]uint16, 260)
	ok, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&pathBuf[0])))
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(pathBuf)
}

// ---- ОПЕРАЦИИ В ФОНЕ ----

func (a *App) runBackground(work func(ctx context.Context)) {
	if a.busy {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelFn = cancel
	a.setBusy(true)
	a.setProgress(0)

	go func() {
		defer func() {
			cancel()
			a.postUI(func() {
				a.setBusy(false)
				a.cancelFn = nil
			})
		}()
		work(ctx)
	}()
}

func (a *App) stopCurrent() {
	if a.busy && a.cancelFn != nil {
		a.cancelFn()
		a.setStatus("Остановка...")
		a.log("🛑 Остановка по запросу...")
	}
}

func (a *App) startCompress() {
	if !a.requireGs() {
		return
	}
	if len(a.compFiles) == 0 {
		a.msgBox("Список файлов пуст.", "PDF Toolkit (Go)", mbOK|mbIconWarning)
		return
	}
	files := append([]string(nil), a.compFiles...)
	profIdx, _, _ := procSendMessageW.Call(a.cbProfile, cbGetCurSel, 0, 0)
	if int(profIdx) < 0 || int(profIdx) >= len(core.Profiles) {
		profIdx = 0
	}
	profile := core.Profiles[profIdx]

	targets := []float64{1.5, 2.0, 3.0, 5.0, 10.0}
	tIdx, _, _ := procSendMessageW.Call(a.cbTarget, cbGetCurSel, 0, 0)
	target := 2.0
	if int(tIdx) >= 0 && int(tIdx) < len(targets) {
		target = targets[tIdx]
	}
	outDir := a.compOutDir

	a.runBackground(func(ctx context.Context) {
		okCount, skipCount := 0, 0
		var totalOrig, totalComp int64

		for i, src := range files {
			if ctx.Err() != nil {
				a.log("⏹ Сжатие остановлено.")
				a.setStatus("⏹ Остановлено пользователем")
				return
			}
			a.setStatus(fmt.Sprintf("[%d/%d] Сжатие: %s", i+1, len(files), filepath.Base(src)))
			a.setProgress(i * 100 / len(files))

			st, err := os.Stat(src)
			if err != nil {
				skipCount++
				continue
			}
			origSize := st.Size()
			totalOrig += origSize

			dir := outDir
			if dir == "" {
				dir = filepath.Dir(src)
			}
			stem := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
			dst := core.UniquePath(filepath.Join(dir, stem+"_compressed.pdf"))

			a.log(fmt.Sprintf("▶ [%d/%d] %s (%s)", i+1, len(files), filepath.Base(src), core.FormatSize(origSize)))

			var size int64
			var compErr error
			if profile.Dpi == 0 {
				size, compErr = a.gs.AutoCompress(ctx, src, dst, target, a.log)
			} else {
				size, compErr = a.gs.Compress(ctx, src, dst, profile)
			}
			if ctx.Err() != nil {
				_ = os.Remove(dst)
				a.log("⏹ Сжатие остановлено.")
				a.setStatus("⏹ Остановлено пользователем")
				return
			}
			if compErr != nil {
				_ = os.Remove(dst)
				totalComp += origSize
				a.log("   ❌ " + compErr.Error())
				skipCount++
				continue
			}
			if size > 0 && size < origSize {
				totalComp += size
				a.log(fmt.Sprintf("   ✅ %s (x%.1f) → %s", core.FormatSize(size), float64(origSize)/float64(size), filepath.Base(dst)))
				okCount++
			} else {
				_ = os.Remove(dst)
				totalComp += origSize
				a.log("   ⚠ Не уменьшился — оригинал не изменён")
				skipCount++
			}
		}

		a.setProgress(100)
		if totalOrig > 0 {
			pct := (1.0 - float64(totalComp)/float64(totalOrig)) * 100.0
			summary := fmt.Sprintf("✅ Готово! Успешно: %d, пропущено: %d | %s → %s (-%.1f%%)",
				okCount, skipCount, core.FormatSize(totalOrig), core.FormatSize(totalComp), pct)
			a.setStatus(summary)
			a.log("📊 " + summary)
			if okCount > 0 {
				a.postUI(func() {
					a.msgBox(fmt.Sprintf("Сжатие завершено!\n\nУспешно: %d\nПропущено: %d\nБыло:  %s\nСтало: %s\nЭкономия: %.1f%%",
						okCount, skipCount, core.FormatSize(totalOrig), core.FormatSize(totalComp), pct),
						"Готово", mbOK|mbIconInformation)
				})
			}
		}
	})
}

func (a *App) startMerge() {
	if !a.requireGs() {
		return
	}
	if len(a.mergeFiles) < 2 {
		a.msgBox("Добавьте минимум 2 PDF файла.", "PDF Toolkit (Go)", mbOK|mbIconWarning)
		return
	}
	files := append([]string(nil), a.mergeFiles...)
	output := strings.TrimSpace(getText(a.edMergeOut))
	if output == "" {
		output = filepath.Join(filepath.Dir(files[0]), "merged.pdf")
	}
	if !strings.EqualFold(filepath.Ext(output), ".pdf") {
		output += ".pdf"
	}
	output = core.UniquePath(output)
	setText(a.edMergeOut, output)

	a.runBackground(func(ctx context.Context) {
		a.setStatus(fmt.Sprintf("Объединение %d файлов...", len(files)))
		a.log(fmt.Sprintf("▶ Объединение %d файлов → %s", len(files), filepath.Base(output)))
		for _, f := range files {
			if st, err := os.Stat(f); err == nil {
				a.log(fmt.Sprintf("   • %s (%s)", filepath.Base(f), core.FormatSize(st.Size())))
			}
		}
		_ = os.MkdirAll(filepath.Dir(output), 0o755)
		if err := a.gs.Merge(ctx, files, output); err != nil {
			if ctx.Err() != nil {
				a.log("⏹ Остановлено.")
				a.setStatus("⏹ Остановлено пользователем")
				return
			}
			a.log("   ❌ " + err.Error())
			a.setStatus("❌ Ошибка объединения — см. лог")
			a.postUI(func() {
				a.msgBox("Не удалось объединить файлы:\n"+err.Error(), "Ошибка", mbOK|mbIconError)
			})
			return
		}
		st, _ := os.Stat(output)
		size := int64(0)
		if st != nil {
			size = st.Size()
		}
		pages, hasPages := a.gs.PageCount(ctx, output)
		a.setProgress(100)
		pStr := "?"
		if hasPages {
			pStr = strconv.Itoa(pages)
		}
		a.log(fmt.Sprintf("   ✅ Готово: %s — %s, %s стр.", filepath.Base(output), core.FormatSize(size), pStr))
		a.setStatus(fmt.Sprintf("✅ Объединено: %s (%s)", filepath.Base(output), core.FormatSize(size)))
		a.postUI(func() {
			a.msgBox(fmt.Sprintf("Файлы объединены!\n\nРезультат: %s\nРазмер: %s, страниц: %s\nПапка: %s",
				filepath.Base(output), core.FormatSize(size), pStr, filepath.Dir(output)),
				"Готово", mbOK|mbIconInformation)
		})
	})
}

func (a *App) startSplit() {
	if !a.requireGs() {
		return
	}
	if len(a.splitFiles) == 0 {
		a.msgBox("Добавьте PDF файлы.", "PDF Toolkit (Go)", mbOK|mbIconWarning)
		return
	}
	sel := getSelectedIndices(a.lbSplit)
	var files []string
	if len(sel) > 0 {
		for _, idx := range sel {
			if idx >= 0 && idx < len(a.splitFiles) {
				files = append(files, a.splitFiles[idx])
			}
		}
	} else {
		files = append([]string(nil), a.splitFiles...)
	}

	mode := 0
	if c, _, _ := procSendMessageW.Call(a.rbChunks, bmGetCheck, 0, 0); c == bstChecked {
		mode = 1
	} else if c, _, _ := procSendMessageW.Call(a.rbRanges, bmGetCheck, 0, 0); c == bstChecked {
		mode = 2
	}

	chunk := 2
	if mode == 1 {
		v, err := strconv.Atoi(strings.TrimSpace(getText(a.edChunkSize)))
		if err != nil || v < 1 {
			a.msgBox("Размер порции должен быть целым числом ≥ 1.", "Ошибка", mbOK|mbIconError)
			return
		}
		chunk = v
	}
	spec := strings.TrimSpace(getText(a.edRanges))
	if mode == 2 && spec == "" {
		a.msgBox("Укажите диапазоны, например: 1-3,5,7-10", "Ошибка", mbOK|mbIconError)
		return
	}
	outBase := a.splitOutDir

	a.runBackground(func(ctx context.Context) {
		okFiles, created := 0, 0
		lastDir := ""

		for i, src := range files {
			if ctx.Err() != nil {
				a.log("⏹ Разделение остановлено.")
				a.setStatus("⏹ Остановлено пользователем")
				return
			}
			a.setStatus(fmt.Sprintf("[%d/%d] Разделение: %s", i+1, len(files), filepath.Base(src)))
			a.setProgress(i * 100 / len(files))

			pages, ok := a.gs.PageCount(ctx, src)
			if !ok || pages <= 0 {
				a.log(fmt.Sprintf("▶ [%d/%d] %s — ❌ Не удалось определить число страниц", i+1, len(files), filepath.Base(src)))
				continue
			}

			baseName := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
			parent := outBase
			if parent == "" {
				parent = filepath.Dir(src)
			}
			outDir := filepath.Join(parent, baseName+"_pages")
			_ = os.MkdirAll(outDir, 0o755)
			lastDir = outDir

			var segments []core.PageRange
			var err error
			switch mode {
			case 0:
				segments = core.EachPage(pages)
			case 1:
				segments = core.Chunks(pages, chunk)
			default:
				segments, err = core.ParseRanges(spec, pages)
			}
			if err != nil {
				a.log("   ❌ " + err.Error())
				continue
			}

			a.log(fmt.Sprintf("▶ [%d/%d] %s (%d стр. → %d файл(ов))", i+1, len(files), filepath.Base(src), pages, len(segments)))
			fileOk := true
			for _, seg := range segments {
				if ctx.Err() != nil {
					a.log("⏹ Разделение остановлено.")
					a.setStatus("⏹ Остановлено пользователем")
					return
				}
				var name string
				if seg.First == seg.Last {
					name = fmt.Sprintf("%s_p%03d.pdf", baseName, seg.First)
				} else {
					name = fmt.Sprintf("%s_p%03d-%03d.pdf", baseName, seg.First, seg.Last)
				}
				outFile := core.UniquePath(filepath.Join(outDir, name))
				if err := a.gs.Extract(ctx, src, seg, outFile); err != nil {
					a.log("      ❌ " + err.Error())
					fileOk = false
					break
				}
				st, _ := os.Stat(outFile)
				sz := int64(0)
				if st != nil {
					sz = st.Size()
				}
				created++
				a.log(fmt.Sprintf("      ✔ %s (%s)", filepath.Base(outFile), core.FormatSize(sz)))
			}
			if fileOk {
				okFiles++
			}
		}

		a.setProgress(100)
		if okFiles == 0 {
			a.setStatus("❌ Ничего не разделено — см. лог")
			a.postUI(func() {
				a.msgBox("Не удалось разделить файлы. Подробности в логе.", "Ошибка", mbOK|mbIconError)
			})
			return
		}
		a.setStatus(fmt.Sprintf("✅ Готово: %d файл(ов) из %d PDF", created, okFiles))
		a.postUI(func() {
			a.msgBox(fmt.Sprintf("Разделение завершено!\n\nСоздано файлов: %d\nИз PDF: %d из %d\n\nПапка:\n%s",
				created, okFiles, len(files), lastDir),
				"Готово", mbOK|mbIconInformation)
		})
	})
}

func (a *App) handleDropFiles(hDrop uintptr) {
	defer procDragFinish.Call(hDrop)
	if a.busy {
		return
	}
	count, _, _ := procDragQueryFileW.Call(hDrop, 0xFFFFFFFF, 0, 0)
	tab := a.currentTab()
	files, _, _ := a.filesPtr(tab)
	buf := make([]uint16, 1024)
	for i := uintptr(0); i < count; i++ {
		procDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		p := syscall.UTF16ToString(buf)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			addFolderRecursive(files, p)
		} else {
			tryAddFile(files, p)
		}
	}
	a.refreshList(tab)
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	a := globalApp
	if a == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return ret
	}

	switch message {
	case wmAppRunUI:
		a.drainUI()
		return 0

	case wmDropFiles:
		a.handleDropFiles(wParam)
		return 0

	case wmNotify:
		hdr := (*nmhdr)(unsafe.Pointer(lParam))
		if hdr.idFrom == idTabs && hdr.code == tcnSelChange {
			a.switchTab(a.currentTab())
			return 0
		}

	case wmCommand:
		id := int(wParam & 0xFFFF)
		code := int((wParam >> 16) & 0xFFFF)

		switch id {
		case idGsManual:
			a.selectGsDialog()
		case idGsDownload:
			procShellExecuteW.Call(0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16("https://ghostscript.com/releases/gsdnld.html"))), 0, 0, swShow)
		case idStop:
			a.stopCurrent()

		// Compress tab
		case idCompAddFiles:
			a.openPdfDialog(0)
		case idCompAddFolder:
			if dir := a.browseFolderDialog("Выберите папку с PDF"); dir != "" {
				addFolderRecursive(&a.compFiles, dir)
				a.refreshList(0)
			}
		case idCompRemove:
			a.removeSelected(0)
		case idCompClear:
			a.compFiles = nil
			a.refreshList(0)
		case idCompProfile:
			if code == cbnSelChange {
				idx, _, _ := procSendMessageW.Call(a.cbProfile, cbGetCurSel, 0, 0)
				if int(idx) >= 0 && int(idx) < len(core.Profiles) {
					setText(a.lblProfDesc, core.Profiles[idx].Description)
				}
			}
		case idCompPickOut:
			if dir := a.browseFolderDialog("Папка для сжатых файлов"); dir != "" {
				a.compOutDir = dir
				setText(a.edCompOut, dir)
			}
		case idCompResetOut:
			a.compOutDir = ""
			setText(a.edCompOut, "[Рядом с оригиналом, суффикс _compressed]")
		case idCompStart:
			a.startCompress()

		// Merge tab
		case idMergeAddFiles:
			a.openPdfDialog(1)
		case idMergeAddFolder:
			if dir := a.browseFolderDialog("Выберите папку с PDF"); dir != "" {
				addFolderRecursive(&a.mergeFiles, dir)
				a.refreshList(1)
			}
		case idMergeUp:
			a.moveMergeSelected(-1)
		case idMergeDown:
			a.moveMergeSelected(1)
		case idMergeRemove:
			a.removeSelected(1)
		case idMergeClear:
			a.mergeFiles = nil
			a.refreshList(1)
		case idMergeSaveAs:
			if out := a.savePdfDialog(); out != "" {
				setText(a.edMergeOut, out)
			}
		case idMergeStart:
			a.startMerge()

		// Split tab
		case idSplitAddFiles:
			a.openPdfDialog(2)
		case idSplitAddFolder:
			if dir := a.browseFolderDialog("Выберите папку с PDF"); dir != "" {
				addFolderRecursive(&a.splitFiles, dir)
				a.refreshList(2)
			}
		case idSplitRemove:
			a.removeSelected(2)
		case idSplitClear:
			a.splitFiles = nil
			a.refreshList(2)
		case idSplitPickOut:
			if dir := a.browseFolderDialog("Папка для разделённых файлов"); dir != "" {
				a.splitOutDir = dir
				setText(a.edSplitOut, dir)
			}
		case idSplitResetOut:
			a.splitOutDir = ""
			setText(a.edSplitOut, "[Подпапка «<имя>_pages» рядом с файлом]")
		case idSplitStart:
			a.startSplit()
		}
		return 0

	case wmClose:
		if a.busy {
			if a.msgBox("Идёт операция. Остановить и выйти?", "Подтверждение", mbYesNo|mbIconQuestion) == idNo {
				return 0
			}
			a.stopCurrent()
		}
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return ret

	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return ret
}

func main() {
	procCoInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED

	icc := initCommonControlsEx{
		dwSize: uint32(unsafe.Sizeof(initCommonControlsEx{})),
		dwICC:  0x0000FFFF,
	}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

	hInst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, 32512) // IDC_ARROW
	icon, _, _ := procLoadIconW.Call(0, 32512)     // IDI_APPLICATION

	className := u16("PdfToolkitGoMainWindow")
	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		hIcon:         icon,
		hCursor:       cursor,
		hbrBackground: 16, // COLOR_BTNFACE + 1
		lpszClassName: className,
		hIconSm:       icon,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	app := &App{
		hInstance: hInst,
		fontUI:    makeFont(-15, 400, "Segoe UI"),
		fontBold:  makeFont(-15, 700, "Segoe UI"),
		fontMono:  makeFont(-14, 400, "Consolas"),
		gs:        core.NewGhostscriptService(""),
	}
	globalApp = app

	hwnd, _, _ := procCreateWindowExW.Call(
		wsExAcceptFiles,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(u16("PDF Toolkit 2.0 (Go) — сжатие · объединение · разделение"))),
		uintptr(wsOverlappedWindow&^(0x00040000|0x00010000)), // фиксированное аккуратное окно
		120, 80, 1000, 765,
		0, 0, hInst, 0,
	)
	app.hwnd = hwnd

	app.buildUI()
	procDragAcceptFiles.Call(hwnd, 1)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	app.initBundledAsync()

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if m.message == wmKeyDown {
			switch m.wParam {
			case 'O':
				st, _, _ := procGetKeyState.Call(vkControl)
				if int16(st) < 0 && !app.busy {
					app.openPdfDialog(app.currentTab())
					continue
				}
			case vkF5:
				if !app.busy {
					switch app.currentTab() {
					case 0:
						app.startCompress()
					case 1:
						app.startMerge()
					default:
						app.startSplit()
					}
					continue
				}
			case vkEscape:
				if app.busy {
					app.stopCurrent()
					continue
				}
			case vkDelete:
				if !app.busy {
					tab := app.currentTab()
					_, lb, _ := app.filesPtr(tab)
					if m.hwnd == lb {
						app.removeSelected(tab)
						continue
					}
				}
			}
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
