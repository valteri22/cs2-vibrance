package main

import (
	"encoding/binary"
	"testing"
)

func TestPackResource(t *testing.T) {
	// A small fake ICO is enough to test the resource format.
	ico := make([]byte, 6+16+4)
	le.PutUint16(ico[2:4], 1)
	le.PutUint16(ico[4:6], 1)
	ico[6] = 16
	ico[7] = 16
	le.PutUint32(ico[14:18], 4)
	le.PutUint32(ico[18:22], 22)
	copy(ico[22:], []byte{1, 2, 3, 4})
	imgs, err := parseICO(ico)
	if err != nil {
		t.Fatal(err)
	}
	data := coff(buildResource(imgs))
	if got := binary.LittleEndian.Uint16(data[0:2]); got != 0x8664 {
		t.Fatalf("COFF machine: %x", got)
	}
	if len(imgs) != 1 || len(data) < 100 {
		t.Fatal("missing icon data")
	}
}
