package hrm

import "testing"

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in   []byte
		bpm  int
		ok   bool
		name string
	}{
		{[]byte{0x00, 72}, 72, true, "uint8"},
		{[]byte{0x16, 140, 0x10, 0x03}, 140, true, "uint8 with contact bits and RR interval"},
		{[]byte{0x01, 0x2C, 0x01}, 300, true, "uint16"},
		{[]byte{0x01, 0x2C}, 0, false, "truncated uint16"},
		{[]byte{0x00}, 0, false, "empty"},
	} {
		bpm, ok := Parse(tc.in)
		if bpm != tc.bpm || ok != tc.ok {
			t.Errorf("%s: Parse(% X) = %d, %v; want %d, %v", tc.name, tc.in, bpm, ok, tc.bpm, tc.ok)
		}
	}
}
