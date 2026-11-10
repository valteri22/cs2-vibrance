package main

// packicon generates a Windows AMD64 .syso COFF resource object from an ICO.
// It runs at build time only. Windows Explorer and the app's tray icon then
// use the same group icon (resource ID 1). No third-party tool is necessary.
//
// Usage: go run ./tools/packicon

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

var le = binary.LittleEndian

type icon struct {
	info []byte // 16-byte ICONDIRENTRY from ICO
	data []byte
}

func put16(b []byte, at int, v uint16) { le.PutUint16(b[at:], v) }
func put32(b []byte, at int, v uint32) { le.PutUint32(b[at:], v) }

func parseICO(b []byte) ([]icon, error) {
	if len(b) < 6 || le.Uint16(b[:2]) != 0 || le.Uint16(b[2:4]) != 1 {
		return nil, errors.New("not an ICO file")
	}
	count := int(le.Uint16(b[4:6]))
	if count < 1 || count > 100 || len(b) < 6+16*count {
		return nil, errors.New("invalid ICO image count")
	}
	out := make([]icon, count)
	for i := range out {
		e := b[6+i*16 : 6+(i+1)*16]
		start, size := uint64(le.Uint32(e[12:])), uint64(le.Uint32(e[8:]))
		if size == 0 || start+size > uint64(len(b)) {
			return nil, fmt.Errorf("ICO image %d exceeds file bounds", i)
		}
		out[i] = icon{info: e, data: b[int(start):int(start+size)]}
	}
	return out, nil
}

type resource struct {
	data   []byte
	relocs []uint32
}

func (r *resource) alloc(n int) int {
	for len(r.data)%4 != 0 {
		r.data = append(r.data, 0)
	}
	off := len(r.data)
	r.data = append(r.data, make([]byte, n)...)
	return off
}
func (r *resource) directory(entries int) int {
	off := r.alloc(16 + 8*entries)
	put16(r.data, off+14, uint16(entries)) // NumberOfIdEntries
	return off
}
func (r *resource) entry(dir, index int, id, target uint32, isDirectory bool) {
	at := dir + 16 + index*8
	put32(r.data, at, id)
	if isDirectory {
		target |= 0x80000000
	}
	put32(r.data, at+4, target)
}
func (r *resource) dataEntry(payload []byte) int {
	entry := r.alloc(16)
	start := r.alloc(len(payload))
	copy(r.data[start:], payload)
	put32(r.data, entry, uint32(start)) // COFF relocation adds .rsrc RVA
	put32(r.data, entry+4, uint32(len(payload)))
	r.relocs = append(r.relocs, uint32(entry))
	return entry
}

func buildResource(icons []icon) *resource {
	r := &resource{}
	root := r.directory(2)
	iconDir := r.directory(len(icons))
	groupDir := r.directory(1)
	r.entry(root, 0, 3, uint32(iconDir), true)   // RT_ICON
	r.entry(root, 1, 14, uint32(groupDir), true) // RT_GROUP_ICON

	for i, item := range icons {
		lang := r.directory(1)
		r.entry(iconDir, i, uint32(i+1), uint32(lang), true)
		data := r.dataEntry(item.data)
		r.entry(lang, 0, 0x409, uint32(data), false)
	}

	// GRPICONDIR: 6 bytes + 14-byte GRPICONDIRENTRY per image.
	group := make([]byte, 6+14*len(icons))
	put16(group, 2, 1)
	put16(group, 4, uint16(len(icons)))
	for i, item := range icons {
		at := 6 + 14*i
		copy(group[at:at+12], item.info[:12])
		put16(group, at+12, uint16(i+1))
	}
	lang := r.directory(1)
	r.entry(groupDir, 0, 1, uint32(lang), true)
	data := r.dataEntry(group)
	r.entry(lang, 0, 0x409, uint32(data), false)
	return r
}

func coff(r *resource) []byte {
	for len(r.data)%4 != 0 {
		r.data = append(r.data, 0)
	}
	const headerSize = 20 + 40
	rawSize := len(r.data)
	relocPtr := headerSize + rawSize
	symbolPtr := relocPtr + 10*len(r.relocs)
	out := make([]byte, symbolPtr+36+4) // 2 COFF symbol records + 4-byte string table

	// COFF file header (AMD64, one .rsrc section).
	put16(out, 0, 0x8664)
	put16(out, 2, 1)
	put32(out, 8, uint32(symbolPtr))
	put32(out, 12, 2)

	// IMAGE_SECTION_HEADER, initialized read-only data.
	copy(out[20:28], []byte(".rsrc"))
	put32(out, 20+16, uint32(rawSize))
	put32(out, 20+20, headerSize)
	put32(out, 20+24, uint32(relocPtr))
	put16(out, 20+32, uint16(len(r.relocs)))
	put32(out, 20+36, 0x40000040)
	copy(out[headerSize:], r.data)

	for i, address := range r.relocs {
		at := relocPtr + i*10
		put32(out, at, address)
		put32(out, at+4, 0)      // section symbol index
		put16(out, at+8, 0x0003) // IMAGE_REL_AMD64_ADDR32NB
	}

	// Section symbol and its auxiliary definition.
	copy(out[symbolPtr:symbolPtr+8], []byte(".rsrc"))
	put16(out, symbolPtr+12, 1)
	out[symbolPtr+16] = 3 // IMAGE_SYM_CLASS_STATIC
	out[symbolPtr+17] = 1 // one aux record
	put32(out, symbolPtr+18, uint32(rawSize))
	put16(out, symbolPtr+18+4, uint16(len(r.relocs)))
	put32(out, symbolPtr+36, 4) // empty COFF string table
	return out
}

func main() {
	b, err := os.ReadFile("assets/icon.ico")
	if err != nil {
		panic(err)
	}
	icons, err := parseICO(b)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("rsrc.syso", coff(buildResource(icons)), 0644); err != nil {
		panic(err)
	}
	fmt.Printf("Generated rsrc.syso from %d icon sizes (build artifact; do not commit).\n", len(icons))
}
