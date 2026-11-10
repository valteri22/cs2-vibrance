//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// NV_DISPLAY_DVC_INFO_EX_V1: 20 bytes; verified against NVAPI wrappers.
type dvcInfo struct {
	Version uint32
	Current int32
	Min     int32
	Max     int32
	Default int32
}
type nvidia struct {
	dll  syscall.Handle
	init uintptr
	enum uintptr
	get  uintptr
	set  uintptr
}

func makeNVAPI() (*nvidia, error) {
	dll, err := syscall.LoadLibrary("nvapi64.dll")
	if err != nil {
		return nil, errors.New("NVIDIA driver not found: nvapi64.dll")
	}
	query, err := syscall.GetProcAddress(dll, "nvapi_QueryInterface")
	if err != nil {
		syscall.FreeLibrary(dll)
		return nil, errors.New("driver missing nvapi_QueryInterface")
	}
	resolve := func(id uintptr) uintptr { f, _, _ := syscall.SyscallN(query, id); return f }
	n := &nvidia{dll: dll, init: resolve(0x0150E828), enum: resolve(0x9ABDD40D), get: resolve(0x0E45002D), set: resolve(0x4A82C2B1)}
	if n.init == 0 || n.enum == 0 || n.get == 0 || n.set == 0 {
		n.close()
		return nil, errors.New("driver does not expose Digital Vibrance NVAPI")
	}
	status, _, _ := syscall.SyscallN(n.init)
	if int32(status) != 0 {
		n.close()
		return nil, fmt.Errorf("NVAPI init error %d", int32(status))
	}
	if len(n.handles()) == 0 {
		n.close()
		return nil, errors.New("no NVIDIA-controlled displays detected")
	}
	return n, nil
}
func (n *nvidia) close() {
	if n != nil && n.dll != 0 {
		syscall.FreeLibrary(n.dll)
		n.dll = 0
	}
}
func (n *nvidia) handles() []uintptr {
	var out []uintptr
	for i := 0; i < 32; i++ {
		var h uintptr
		status, _, _ := syscall.SyscallN(n.enum, uintptr(i), uintptr(unsafe.Pointer(&h)))
		if int32(status) != 0 {
			break
		}
		if h != 0 {
			out = append(out, h)
		}
	}
	return out
}
func (n *nvidia) info(h uintptr) (dvcInfo, error) {
	v := dvcInfo{Version: uint32(unsafe.Sizeof(dvcInfo{})) | (1 << 16)}
	status, _, _ := syscall.SyscallN(n.get, h, 0, uintptr(unsafe.Pointer(&v)))
	runtime.KeepAlive(&v)
	if int32(status) != 0 {
		return v, fmt.Errorf("NVAPI read error %d", int32(status))
	}
	if v.Max <= v.Min || v.Default < v.Min || v.Default > v.Max {
		return v, errors.New("invalid driver Digital Vibrance range")
	}
	return v, nil
}

// Control Panel's default/neutral point is 50%. Interpolate around the
// driver-reported default rather than assuming a fixed raw NVAPI scale.
func levelForPercent(percent int, v dvcInfo) int32 {
	p := clamp(percent)
	lo, mid, hi := float64(v.Min), float64(v.Default), float64(v.Max)
	var x float64
	if p <= 50 {
		x = lo + (mid-lo)*float64(p)/50
	} else {
		x = mid + (hi-mid)*float64(p-50)/50
	}
	if x < 0 {
		return int32(x - 0.5)
	}
	return int32(x + 0.5)
}
func (n *nvidia) setRaw(h uintptr, raw int32) error {
	v, err := n.info(h)
	if err != nil {
		return err
	}
	if raw < v.Min || raw > v.Max {
		return fmt.Errorf("saved level %d outside driver range", raw)
	}
	if v.Current == raw {
		return nil
	}
	v.Current = raw
	status, _, _ := syscall.SyscallN(n.set, h, 0, uintptr(unsafe.Pointer(&v)))
	runtime.KeepAlive(&v)
	if int32(status) != 0 {
		return fmt.Errorf("NVAPI set error %d", int32(status))
	}
	return nil
}
func (n *nvidia) capture() ([]displaySnapshot, error) {
	var out []displaySnapshot
	for i, h := range n.handles() {
		v, err := n.info(h)
		if err == nil {
			out = append(out, displaySnapshot{Index: i, RawLevel: v.Current})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("cannot read Digital Vibrance on any display")
	}
	return out, nil
}
func (n *nvidia) apply(s []displaySnapshot, percent int) error {
	handles := n.handles()
	var failures []string
	for _, d := range s {
		if d.Index >= len(handles) {
			failures = append(failures, fmt.Sprintf("display %d disappeared", d.Index))
			continue
		}
		v, err := n.info(handles[d.Index])
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if err := n.setRaw(handles[d.Index], levelForPercent(percent, v)); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}
func (n *nvidia) matches(s []displaySnapshot, percent int) bool {
	handles := n.handles()
	if len(s) == 0 {
		return false
	}
	for _, d := range s {
		if d.Index >= len(handles) {
			return false
		}
		v, err := n.info(handles[d.Index])
		if err != nil || v.Current != levelForPercent(percent, v) {
			return false
		}
	}
	return true
}
func (n *nvidia) restore(s []displaySnapshot) error {
	handles := n.handles()
	var failures []string
	for _, d := range s {
		if d.Index >= len(handles) {
			failures = append(failures, fmt.Sprintf("display %d disappeared", d.Index))
			continue
		}
		if err := n.setRaw(handles[d.Index], d.RawLevel); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}
