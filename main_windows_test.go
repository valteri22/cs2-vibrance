//go:build windows

package main

import "testing"

func TestVibranceConversion(t *testing.T) {
	v := dvcInfo{Min: -100, Default: 0, Max: 100}
	for _, tc := range []struct {
		percent int
		raw     int32
	}{
		{0, -100}, {25, -50}, {50, 0}, {75, 50}, {100, 100},
	} {
		if got := levelForPercent(tc.percent, v); got != tc.raw {
			t.Errorf("levelForPercent(%d)=%d; want %d", tc.percent, got, tc.raw)
		}
	}
}
