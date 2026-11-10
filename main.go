//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	appName            = "CS2 Vibrance"
	className          = "CS2VibranceWindow"
	regRunPath         = `Software\Microsoft\Windows\CurrentVersion\Run`
	WM_CREATE          = 0x0001
	WM_DESTROY         = 0x0002
	WM_CLOSE           = 0x0010
	WM_COMMAND         = 0x0111
	WM_TIMER           = 0x0113
	WM_APP             = 0x8000
	WM_LBUTTONDBLCLK   = 0x0203
	WM_LBUTTONUP       = 0x0202
	WM_RBUTTONUP       = 0x0205
	WS_CAPTION         = 0x00C00000
	WS_SYSMENU         = 0x00080000
	WS_MINIMIZEBOX     = 0x00020000
	SW_HIDE            = 0
	SW_SHOW            = 5
	NIF_MESSAGE        = 1
	NIF_ICON           = 2
	NIF_TIP            = 4
	NIM_ADD            = 0
	NIM_DELETE         = 2
	TPM_RIGHTBUTTON    = 2
	TPM_BOTTOMALIGN    = 0x0020
	MF_STRING          = 0
	MF_SEPARATOR       = 0x0800
	ID_SETTINGS        = 500
	ID_EXIT            = 501
	TRAY_ID            = 1
	WM_TRAY            = WM_APP + 1
	WM_OPEN            = WM_APP + 2
	TIMER_ID           = 1
	TH32CS_SNAPPROCESS = 2
	HKEY_CURRENT_USER  = 0xffffffff80000001
	KEY_SET_VALUE      = 2
	REG_SZ             = 1
)

type settings struct {
	Game      int  `json:"game_vibrance"`
	Autostart bool `json:"start_with_windows"`
}

type displaySnapshot struct {
	Index    int   `json:"display_index"`
	RawLevel int32 `json:"raw_level"`
}

type restoreState struct {
	Displays []displaySnapshot `json:"displays"`
}

var cfg = settings{Game: 80, Autostart: true}
var cfgFile, sessionFile string

func clamp(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}
func ptr(s string) *uint16           { return syscall.StringToUTF16Ptr(s) }
func u16copy(out []uint16, s string) { copy(out, syscall.StringToUTF16(s)) }

func initConfig() error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir = filepath.Join(dir, "CS2Vibrance")
	cfgFile = filepath.Join(dir, "settings.json")
	sessionFile = filepath.Join(dir, "restore-session.json")
	b, err := os.ReadFile(cfgFile)
	if errors.Is(err, os.ErrNotExist) {
		// Only bring over the old settings, not its recovery session.
		old := filepath.Join(filepath.Dir(dir), "CS2VibranceLite", "settings.json")
		if legacy, readErr := os.ReadFile(old); readErr == nil {
			b, err = legacy, nil
		}
	}
	if err == nil {
		// Keep the defaults for any missing fields.
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("settings.json: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg.Game = clamp(cfg.Game)
	return nil
}
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	// Write to a temp file first so a crash is less likely to break recovery.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
func saveConfig() error { return writeJSON(cfgFile, cfg) }
func saveSession(s []displaySnapshot) error {
	if len(s) == 0 {
		return errors.New("refusing empty recovery snapshot")
	}
	return writeJSON(sessionFile, restoreState{Displays: s})
}
func loadSession() ([]displaySnapshot, error) {
	b, err := os.ReadFile(sessionFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rs restoreState
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil, fmt.Errorf("invalid restore-session.json: %w", err)
	}
	if len(rs.Displays) == 0 {
		return nil, errors.New("empty restore-session.json")
	}
	// Don't restore from a broken snapshot.
	for _, s := range rs.Displays {
		if s.Index < 0 || s.Index >= 32 {
			return nil, errors.New("invalid display index in recovery snapshot")
		}
	}
	return rs.Displays, nil
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	pRegisterClassW           = user32.NewProc("RegisterClassW")
	pCreateWindowExW          = user32.NewProc("CreateWindowExW")
	pDefWindowProcW           = user32.NewProc("DefWindowProcW")
	pPostMessageW             = user32.NewProc("PostMessageW")
	pShowWindow               = user32.NewProc("ShowWindow")
	pUpdateWindow             = user32.NewProc("UpdateWindow")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pLoadIconW                = user32.NewProc("LoadIconW")
	pLoadCursorW              = user32.NewProc("LoadCursorW")
	pFindWindowW              = user32.NewProc("FindWindowW")
	pGetMessageW              = user32.NewProc("GetMessageW")
	pTranslateMessage         = user32.NewProc("TranslateMessage")
	pDispatchMessageW         = user32.NewProc("DispatchMessageW")
	pPostQuitMessage          = user32.NewProc("PostQuitMessage")
	pSetTimer                 = user32.NewProc("SetTimer")
	pKillTimer                = user32.NewProc("KillTimer")
	pCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	pAppendMenuW              = user32.NewProc("AppendMenuW")
	pTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	pDestroyMenu              = user32.NewProc("DestroyMenu")
	pGetCursorPos             = user32.NewProc("GetCursorPos")
	pMessageBoxW              = user32.NewProc("MessageBoxW")
	pGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
	pCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	pProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	pProcess32NextW           = kernel32.NewProc("Process32NextW")
	pCloseHandle              = kernel32.NewProc("CloseHandle")
	pCreateMutexW             = kernel32.NewProc("CreateMutexW")
	pShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	pRegCreateKeyExW          = advapi32.NewProc("RegCreateKeyExW")
	pRegSetValueExW           = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValueW          = advapi32.NewProc("RegDeleteValueW")
	pRegCloseKey              = advapi32.NewProc("RegCloseKey")
	pCreateFontW              = gdi32.NewProc("CreateFontW")
	pDeleteObject             = gdi32.NewProc("DeleteObject")
)

type windowClass struct {
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
}
type message struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
	Private uint32
}
type point struct{ X, Y int32 }
type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}
type processEntry struct {
	Size          uint32
	Usage         uint32
	ProcessID     uint32
	DefaultHeapID uintptr
	ModuleID      uint32
	Threads       uint32
	ParentID      uint32
	Priority      int32
	Flags         uint32
	ExeFile       [260]uint16
}

// Only check process names. No need to touch game memory.
func cs2Running() bool {
	snap, _, _ := pCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == 0 || snap == ^uintptr(0) {
		return false
	}
	defer pCloseHandle.Call(snap)
	var e processEntry
	e.Size = uint32(unsafe.Sizeof(e))
	ok, _, _ := pProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for ok != 0 {
		if strings.EqualFold(syscall.UTF16ToString(e.ExeFile[:]), "cs2.exe") {
			return true
		}
		ok, _, _ = pProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return false
}
func setStartup(on bool) error {
	var key uintptr
	status, _, _ := pRegCreateKeyExW.Call(HKEY_CURRENT_USER, uintptr(unsafe.Pointer(ptr(regRunPath))), 0, 0, 0, KEY_SET_VALUE, 0, uintptr(unsafe.Pointer(&key)), 0)
	if status != 0 {
		return fmt.Errorf("cannot open Windows Run key: %d", status)
	}
	defer pRegCloseKey.Call(key)
	name := ptr("CS2Vibrance")
	if !on {
		pRegDeleteValueW.Call(key, uintptr(unsafe.Pointer(name)))
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	value := syscall.StringToUTF16(`"` + exe + `" --background`)
	status, _, _ = pRegSetValueExW.Call(key, uintptr(unsafe.Pointer(name)), 0, REG_SZ, uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)*2))
	if status != 0 {
		return fmt.Errorf("cannot set Windows startup: %d", status)
	}
	return nil
}

type appState struct {
	instance, hwnd, icon, font, smallFont, boldFont uintptr
	uiStatus, uiDetail                              string
	uiValue                                         int
	dragging                                        bool
	focusItem                                       int
	nv                                              *nvidia
	original                                        []displaySnapshot
	lastApplied                                     int
	lastVerified, lastInit, lastRestore             time.Time
	lastError                                       string
	closing                                         bool
}

var app appState

func showSettings() {
	pShowWindow.Call(app.hwnd, SW_SHOW)
	pUpdateWindow.Call(app.hwnd)
	pSetForegroundWindow.Call(app.hwnd)
}
func hideSettings() { pShowWindow.Call(app.hwnd, SW_HIDE) }
func detail(status, info string) {
	if app.uiStatus != status || app.uiDetail != info {
		app.uiStatus, app.uiDetail = status, info
		if app.hwnd != 0 {
			invalidateUI()
		}
	}
}

func ensureNVAPI() bool {
	if app.nv != nil {
		return true
	}
	if !app.lastInit.IsZero() && time.Since(app.lastInit) < 10*time.Second {
		return false
	}
	app.lastInit = time.Now()
	var err error
	app.nv, err = makeNVAPI()
	if err != nil {
		app.lastError = err.Error()
		return false
	}
	return true
}
func tick(force bool) {
	running := cs2Running()
	// Leave the desktop colors alone until CS2 starts.
	if !running && len(app.original) == 0 {
		app.lastApplied = -1
		detail("Waiting for CS2", "Desktop color setting left untouched.")
		return
	}
	if !ensureNVAPI() {
		detail("NVIDIA Digital Vibrance unavailable", app.lastError)
		return
	}
	if running {
		if len(app.original) == 0 {
			var err error
			original, err := app.nv.capture()
			if err != nil {
				app.lastError = err.Error()
				detail("Unable to read original colors", app.lastError)
				return
			}
			// Save the original colors before changing anything.
			if err = saveSession(original); err != nil {
				app.lastError = "Could not save original levels: " + err.Error()
				detail("Vibrance not changed", app.lastError)
				return
			}
			app.original = original
			app.lastApplied = -1
		}
		needsApply := force || app.lastApplied != cfg.Game
		if !needsApply && time.Since(app.lastVerified) >= 15*time.Second {
			needsApply = !app.nv.matches(app.original, cfg.Game)
			app.lastVerified = time.Now()
		}
		if needsApply {
			if err := app.nv.apply(app.original, cfg.Game); err != nil {
				app.lastError = err.Error()
				detail("Unable to apply Digital Vibrance", app.lastError)
				app.lastApplied = -1
				return
			}
			app.lastApplied = cfg.Game
			app.lastVerified = time.Now()
			app.lastError = ""
		}
		detail(fmt.Sprintf("CS2 running - %d%% Digital Vibrance", cfg.Game), "Restore original colors automatically when CS2 closes.")
		return
	}
	// CS2 closed. Restore the previous colors.
	if !app.lastRestore.IsZero() && time.Since(app.lastRestore) < 5*time.Second {
		return
	}
	app.lastRestore = time.Now()
	if err := app.nv.restore(app.original); err != nil {
		app.lastError = err.Error()
		detail("Restoring desktop colors...", app.lastError)
		return
	}
	if err := os.Remove(sessionFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		app.lastError = "Restored, but could not clear restore-session.json: " + err.Error()
		detail("Original colors restored", app.lastError)
		return
	}
	app.original = nil
	app.lastApplied = -1
	app.lastError = ""
	detail("Waiting for CS2", "Original desktop colors restored; no further driver changes.")
}

func trayMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	pAppendMenuW.Call(menu, MF_STRING, ID_SETTINGS, uintptr(unsafe.Pointer(ptr("Open settings"))))
	pAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	pAppendMenuW.Call(menu, MF_STRING, ID_EXIT, uintptr(unsafe.Pointer(ptr("Quit (restore original colors)"))))
	var xy point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&xy)))
	pSetForegroundWindow.Call(app.hwnd)
	pTrackPopupMenu.Call(menu, TPM_RIGHTBUTTON|TPM_BOTTOMALIGN, uintptr(xy.X), uintptr(xy.Y), 0, app.hwnd, 0)
	pDestroyMenu.Call(menu)
}
func trayIcon(action uintptr) {
	var icon notifyIconData
	icon.CbSize = uint32(unsafe.Sizeof(icon))
	icon.HWnd = app.hwnd
	icon.UID = TRAY_ID
	icon.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	icon.UCallbackMessage = WM_TRAY
	icon.HIcon = app.icon
	u16copy(icon.SzTip[:], appName)
	pShellNotifyIconW.Call(action, uintptr(unsafe.Pointer(&icon)))
}
func shutdown() {
	if app.closing {
		return
	}
	// Keep the recovery file if restoring fails.
	if len(app.original) > 0 {
		if ensureNVAPI() {
			if err := app.nv.restore(app.original); err != nil {
				pMessageBoxW.Call(app.hwnd, uintptr(unsafe.Pointer(ptr("Could not restore original colors: "+err.Error()+"\nRecovery snapshot retained for next launch."))), uintptr(unsafe.Pointer(ptr(appName))), 0)
			} else {
				_ = os.Remove(sessionFile)
			}
		}
	}
	app.closing = true
	pKillTimer.Call(app.hwnd, TIMER_ID)
	trayIcon(NIM_DELETE)
	if app.nv != nil {
		app.nv.close()
	}
	for _, f := range []uintptr{app.font, app.smallFont, app.boldFont} {
		if f != 0 {
			pDeleteObject.Call(f)
		}
	}
	pPostQuitMessage.Call(0)
}
func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_CREATE:
		app.hwnd = hwnd
		setupUI()
		return 0
	case WM_OPEN:
		showSettings()
		return 0
	case WM_CLOSE:
		hideSettings()
		return 0
	case WM_DESTROY:
		shutdown()
		return 0
	case WM_TRAY:
		if lParam == WM_LBUTTONDBLCLK || lParam == WM_LBUTTONUP {
			showSettings()
		}
		if lParam == WM_RBUTTONUP {
			trayMenu()
		}
		return 0
	case WM_TIMER:
		if wParam == TIMER_ID {
			tick(false)
		}
		return 0

	case WM_PAINT:
		paintUI(hwnd)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_LBUTTONDOWN, WM_LBUTTONUP, WM_MOUSEMOVE, WM_CAPTURECHANGED, WM_KEYDOWN, WM_SETFOCUS, WM_KILLFOCUS:
		if handleUIMessage(msg, wParam, lParam) {
			return 0
		}
	case WM_COMMAND:
		switch uint16(wParam & 0xffff) {
		case ID_SETTINGS:
			showSettings()
			return 0
		case ID_EXIT:
			shutdown()
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}
func containsArg(s string) bool {
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, s) {
			return true
		}
	}
	return false
}
func main() {
	runtime.LockOSThread() // Keep Win32 events and NVAPI on the same thread.
	if err := initConfig(); err != nil {
		pMessageBoxW.Call(0, uintptr(unsafe.Pointer(ptr(err.Error()))), uintptr(unsafe.Pointer(ptr(appName))), 0)
		return
	}
	mutexName := ptr("Local\\CS2Vibrance_SingleInstance")
	mutex, _, lastErr := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if mutex != 0 && lastErr == syscall.Errno(183) {
		existing, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(ptr(className))), 0)
		if existing != 0 {
			pPostMessageW.Call(existing, WM_OPEN, 0, 0)
		}
		pCloseHandle.Call(mutex)
		return
	}
	defer func() {
		if mutex != 0 {
			pCloseHandle.Call(mutex)
		}
	}()
	original, err := loadSession()
	if err != nil {
		pMessageBoxW.Call(0, uintptr(unsafe.Pointer(ptr("Recovery data needs attention: "+err.Error()))), uintptr(unsafe.Pointer(ptr(appName))), 0)
		return
	}
	app.original = original
	if err := saveConfig(); err != nil {
		pMessageBoxW.Call(0, uintptr(unsafe.Pointer(ptr(err.Error()))), uintptr(unsafe.Pointer(ptr(appName))), 0)
		return
	}
	startupErr := setStartup(cfg.Autostart)

	instance, _, _ := pGetModuleHandleW.Call(0)
	icon, _, _ := pLoadIconW.Call(instance, 1)
	if icon == 0 {
		icon, _, _ = pLoadIconW.Call(0, 32512)
	}
	cursor, _, _ := pLoadCursorW.Call(0, 32512)
	app.instance = instance
	app.icon = icon
	cls := windowClass{WndProc: syscall.NewCallback(wndProc), Instance: instance, Icon: icon, Cursor: cursor, Background: 6, ClassName: ptr(className)}
	atom, _, _ := pRegisterClassW.Call(uintptr(unsafe.Pointer(&cls)))
	if atom == 0 {
		pMessageBoxW.Call(0, uintptr(unsafe.Pointer(ptr("Unable to register window"))), uintptr(unsafe.Pointer(ptr(appName))), 0)
		return
	}
	hwnd, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(ptr(className))), uintptr(unsafe.Pointer(ptr(appName))), WS_CAPTION|WS_SYSMENU|WS_MINIMIZEBOX, 240, 150, 372, 255, 0, 0, instance, 0)
	if hwnd == 0 {
		return
	}
	app.hwnd = hwnd
	setDarkTitleBar(hwnd)
	app.lastApplied = -1
	trayIcon(NIM_ADD)
	if !containsArg("--background") {
		showSettings()
	}
	if startupErr != nil {
		app.lastError = startupErr.Error()
	}
	tick(false)
	pSetTimer.Call(hwnd, TIMER_ID, 1500, 0)
	var msg message
	for {
		ret, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
