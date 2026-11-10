//go:build windows

package main

// A deliberately small, native Win32 UI. There are no web views, animations,
// background rendering loops, fonts to download, or UI dependencies.
// Mouse and keyboard controls use the same interaction/state path.

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	WM_PAINT          = 0x000F
	WM_ERASEBKGND     = 0x0014
	WM_SETFOCUS       = 0x0007
	WM_KILLFOCUS      = 0x0008
	WM_KEYDOWN        = 0x0100
	WM_MOUSEMOVE      = 0x0200
	WM_LBUTTONDOWN    = 0x0201
	WM_CAPTURECHANGED = 0x0215
	VK_TAB            = 0x09
	VK_SPACE          = 0x20
	VK_LEFT           = 0x25
	VK_RIGHT          = 0x27
	VK_UP             = 0x26
	VK_DOWN           = 0x28
	VK_PRIOR          = 0x21
	VK_NEXT           = 0x22
	VK_HOME           = 0x24
	VK_END            = 0x23
	SRCCOPY           = 0x00CC0020
	TRANSPARENT       = 1
	PS_SOLID          = 0
)

type rect struct{ Left, Top, Right, Bottom int32 }
type paintStruct struct {
	DC      uintptr
	Erase   int32
	Paint   rect
	Restore int32
	Inc     int32
	Extra   [32]byte
}

type size struct{ CX, CY int32 }

var (
	dwmapi                  = syscall.NewLazyDLL("dwmapi.dll")
	pDwmSetWindowAttribute  = dwmapi.NewProc("DwmSetWindowAttribute")
	pInvalidateRect         = user32.NewProc("InvalidateRect")
	pGetClientRect          = user32.NewProc("GetClientRect")
	pBeginPaint             = user32.NewProc("BeginPaint")
	pEndPaint               = user32.NewProc("EndPaint")
	pSetCapture             = user32.NewProc("SetCapture")
	pReleaseCapture         = user32.NewProc("ReleaseCapture")
	pSetFocus               = user32.NewProc("SetFocus")
	pGetFocus               = user32.NewProc("GetFocus")
	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pCreatePen              = gdi32.NewProc("CreatePen")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pTextOutW               = gdi32.NewProc("TextOutW")
	pFillRect               = user32.NewProc("FillRect")
	pRoundRect              = gdi32.NewProc("RoundRect")
	pEllipse                = gdi32.NewProc("Ellipse")
	pBitBlt                 = gdi32.NewProc("BitBlt")
)

// COLORREF expects BGR data, not RGB.
func rgb(r, g, b byte) uintptr { return uintptr(uint32(r) | uint32(g)<<8 | uint32(b)<<16) }

var (
	colorBG      = rgb(19, 21, 25)
	colorText    = rgb(234, 237, 244)
	colorMuted   = rgb(147, 154, 168)
	colorTrack   = rgb(58, 63, 73)
	colorAccent  = rgb(113, 147, 248)
	colorSuccess = rgb(91, 199, 133)
	colorWarning = rgb(243, 172, 93)
	colorBorder  = rgb(49, 53, 61)
)

func setupUI() {
	app.uiValue = cfg.Game
	app.uiStatus = "Waiting for CS2"
	app.font = uiFont(-17, 400)
	app.boldFont = uiFont(-18, 600)
	app.smallFont = uiFont(-14, 400)
}

func uiFont(height int32, weight int32) uintptr {
	f, _, _ := pCreateFontW.Call(uintptr(uint32(height)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(ptr("Segoe UI"))))
	return f
}

func setDarkTitleBar(hwnd uintptr) {
	// Officially supported on Windows 11; older versions simply ignore errors.
	var on int32 = 1
	pDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&on)), unsafe.Sizeof(on))
}

func invalidateUI() { pInvalidateRect.Call(app.hwnd, 0, 0) }

func drawText(dc uintptr, value string, x, y int32, font, color uintptr) {
	if font != 0 {
		pSelectObject.Call(dc, font)
	}
	pSetTextColor.Call(dc, color)
	w := syscall.StringToUTF16(value)
	pTextOutW.Call(dc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)-1))
}

func fillRect(dc uintptr, r rect, color uintptr) {
	brush, _, _ := pCreateSolidBrush.Call(color)
	pFillRect.Call(dc, uintptr(unsafe.Pointer(&r)), brush)
	pDeleteObject.Call(brush)
}

func shape(dc uintptr, r rect, radius int32, color uintptr, ellipse bool) {
	brush, _, _ := pCreateSolidBrush.Call(color)
	pen, _, _ := pCreatePen.Call(PS_SOLID, 1, color)
	oldB, _, _ := pSelectObject.Call(dc, brush)
	oldP, _, _ := pSelectObject.Call(dc, pen)
	if ellipse {
		pEllipse.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom))
	} else {
		pRoundRect.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius), uintptr(radius))
	}
	pSelectObject.Call(dc, oldP)
	pSelectObject.Call(dc, oldB)
	pDeleteObject.Call(pen)
	pDeleteObject.Call(brush)
}

func paintUI(hwnd uintptr) {
	var ps paintStruct
	dc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return
	}
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var r rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	width, height := r.Right, r.Bottom
	if width <= 0 || height <= 0 {
		return
	}
	mem, _, _ := pCreateCompatibleDC.Call(dc)
	bmp, _, _ := pCreateCompatibleBitmap.Call(dc, uintptr(width), uintptr(height))
	if mem == 0 || bmp == 0 {
		if bmp != 0 {
			pDeleteObject.Call(bmp)
		}
		if mem != 0 {
			pDeleteDC.Call(mem)
		}
		return
	}
	old, _, _ := pSelectObject.Call(mem, bmp)
	defer func() {
		pBitBlt.Call(dc, 0, 0, uintptr(width), uintptr(height), mem, 0, 0, SRCCOPY)
		pSelectObject.Call(mem, old)
		pDeleteObject.Call(bmp)
		pDeleteDC.Call(mem)
	}()
	pSetBkMode.Call(mem, TRANSPARENT)
	fillRect(mem, r, colorBG)

	// One adjustable value; neutral/default is annotated below the slider.
	drawText(mem, "Digital Vibrance", 22, 22, app.boldFont, colorText)
	drawText(mem, fmt.Sprintf("%d%%", app.uiValue), width-74, 21, app.boldFont, colorText)
	left, right := int32(23), width-23
	sliderY := int32(78)
	shape(mem, rect{left, sliderY, right, sliderY + 6}, 7, colorTrack, false)
	pos := left + (right-left)*int32(app.uiValue)/100
	if pos > left {
		shape(mem, rect{left, sliderY, pos, sliderY + 6}, 7, colorAccent, false)
	}
	if app.focusItem == 1 && uiHasFocus() {
		shape(mem, rect{pos - 10, sliderY - 7, pos + 10, sliderY + 13}, 20, colorText, true)
	}
	shape(mem, rect{pos - 7, sliderY - 4, pos + 7, sliderY + 10}, 14, colorAccent, true)
	drawText(mem, "0", left, 102, app.smallFont, colorMuted)
	drawText(mem, "50% = neutral", width/2-47, 102, app.smallFont, colorMuted)
	drawText(mem, "100", right-23, 102, app.smallFont, colorMuted)

	fillRect(mem, rect{22, 134, width - 22, 135}, colorBorder)
	cb := rect{23, 153, 39, 169}
	if cfg.Autostart {
		shape(mem, cb, 5, colorAccent, false)
		drawText(mem, "✓", 24, 150, app.font, colorBG)
	} else {
		shape(mem, cb, 5, colorTrack, false)
		shape(mem, rect{25, 155, 37, 167}, 3, colorBG, false)
	}
	if app.focusItem == 2 && uiHasFocus() {
		shape(mem, rect{19, 149, 43, 173}, 5, colorAccent, false)
		shape(mem, cb, 5, colorBG, false)
		if cfg.Autostart {
			shape(mem, rect{25, 155, 37, 167}, 4, colorAccent, false)
			drawText(mem, "✓", 24, 150, app.font, colorText)
		}
	}
	drawText(mem, "Start with Windows", 49, 151, app.font, colorText)

	// Visible system status, no progress bar or unnecessary buttons.
	fillRect(mem, rect{22, 183, width - 22, 184}, colorBorder)
	statusColor := colorMuted
	status := app.uiStatus
	if strings.Contains(status, "running") {
		statusColor = colorSuccess
		status = "CS2 active · vibrance applied"
	}
	if app.lastError != "" {
		statusColor = colorWarning
		status = "Issue detected · click for details"
	}
	shape(mem, rect{24, 199, 32, 207}, 8, statusColor, true)
	drawText(mem, status, 41, 192, app.smallFont, colorMuted)
}

func uiHasFocus() bool {
	focus, _, _ := pGetFocus.Call()
	return focus == app.hwnd
}

func clientWidth() int32 {
	var r rect
	pGetClientRect.Call(app.hwnd, uintptr(unsafe.Pointer(&r)))
	return r.Right
}
func fromMouseX(x int32) int {
	left, right := int32(23), clientWidth()-23
	if x < left {
		x = left
	}
	if x > right {
		x = right
	}
	if right <= left {
		return cfg.Game
	}
	return clamp(int((x-left)*100+(right-left)/2) / int(right-left))
}

func updatePreview(x int32) {
	v := fromMouseX(x)
	if v != app.uiValue {
		app.uiValue = v
		invalidateUI()
	}
}
func commitValue() {
	if app.uiValue == cfg.Game {
		return
	}
	old := cfg.Game
	cfg.Game = app.uiValue
	if err := saveConfig(); err != nil {
		cfg.Game = old
		app.uiValue = old
		app.lastError = err.Error()
		detail("Could not save settings", err.Error())
		return
	}
	app.lastError = ""
	tick(true) // No NVIDIA reads/writes if CS2 is closed and no restore is pending.
	invalidateUI()
}
func toggleStartup() {
	next := !cfg.Autostart
	if err := setStartup(next); err != nil {
		app.lastError = err.Error()
		detail("Could not change startup", err.Error())
		return
	}
	cfg.Autostart = next
	if err := saveConfig(); err != nil {
		app.lastError = err.Error()
		detail("Could not save settings", err.Error())
	} else {
		app.lastError = ""
	}
	invalidateUI()
}

func mousePoint(lparam uintptr) (int32, int32) {
	return int32(int16(uint16(lparam))), int32(int16(uint16(lparam >> 16)))
}
func handleUIMessage(msg uint32, wParam, lParam uintptr) bool {
	switch msg {
	case WM_SETFOCUS, WM_KILLFOCUS:
		invalidateUI()
		return true
	case WM_LBUTTONDOWN:
		x, y := mousePoint(lParam)
		pSetFocus.Call(app.hwnd)
		if y >= 64 && y <= 101 {
			app.focusItem = 1
			app.dragging = true
			pSetCapture.Call(app.hwnd)
			updatePreview(x)
		} else if y >= 143 && y <= 175 {
			app.focusItem = 2
			toggleStartup()
		} else if y >= 189 && app.lastError != "" {
			pMessageBoxW.Call(app.hwnd, uintptr(unsafe.Pointer(ptr(app.lastError))), uintptr(unsafe.Pointer(ptr(appName))), 0)
		}
		invalidateUI()
		return true
	case WM_MOUSEMOVE:
		if app.dragging {
			x, _ := mousePoint(lParam)
			updatePreview(x)
		}
		return true
	case WM_LBUTTONUP:
		if app.dragging {
			x, _ := mousePoint(lParam)
			updatePreview(x)
			app.dragging = false
			pReleaseCapture.Call()
			commitValue()
		}
		return true
	case WM_CAPTURECHANGED:
		if app.dragging {
			app.dragging = false
			commitValue()
		}
		return true
	case WM_KEYDOWN:
		if wParam == VK_TAB {
			if app.focusItem == 1 {
				app.focusItem = 2
			} else {
				app.focusItem = 1
			}
			invalidateUI()
			return true
		}
		if app.focusItem == 2 {
			if wParam == VK_SPACE {
				toggleStartup()
				return true
			}
			return false
		}
		if app.focusItem == 0 {
			app.focusItem = 1
		}
		n := cfg.Game
		switch wParam {
		case VK_LEFT, VK_DOWN:
			n--
		case VK_RIGHT, VK_UP:
			n++
		case VK_PRIOR:
			n += 10
		case VK_NEXT:
			n -= 10
		case VK_HOME:
			n = 0
		case VK_END:
			n = 100
		default:
			return false
		}
		app.uiValue = clamp(n)
		commitValue()
		return true
	}
	return false
}
