package fec

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	page := BasicResistance(37.5)
	frame := Encode(page)

	want := []byte{0xA4, 0x09, 0x4F, 0x05, 0x30, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 75}
	if !bytes.Equal(frame[:12], want) {
		t.Fatalf("frame = % X, want prefix % X", frame, want)
	}
	if got, err := Decode(frame); err != nil || got != page {
		t.Fatalf("Decode = % X, %v", got, err)
	}
}

func TestDecodeErrors(t *testing.T) {
	frame := Encode(TargetPower(200))
	bad := bytes.Clone(frame)
	bad[len(bad)-1] ^= 0xFF
	if _, err := Decode(bad); err != ErrChecksum {
		t.Errorf("bad checksum: err = %v", err)
	}
	if _, err := Decode(frame[:5]); err != ErrShortFrame {
		t.Errorf("short frame: err = %v", err)
	}
	bad = bytes.Clone(frame)
	bad[0] = 0x00
	if _, err := Decode(bad); err != ErrBadSync {
		t.Errorf("bad sync: err = %v", err)
	}
}

func TestParseTrainerData(t *testing.T) {
	// cadence 90, accumulated 0x1234, power 0x123 = 291 W, resistance
	// calibration requested, target status "speed too low", state in use.
	p := [8]byte{0x19, 7, 90, 0x34, 0x12, 0x23, 0x21, 0x31}
	d := ParseTrainerData(p)
	if d.Cadence != 90 || d.Power != 291 || d.AccumulatedPower != 0x1234 {
		t.Errorf("got %+v", d)
	}
	if !d.ResistanceCalib || d.PowerCalibration || d.TargetStatus != TargetSpeedLow || d.State != StateInUse {
		t.Errorf("flags: %+v", d)
	}

	invalid := ParseTrainerData([8]byte{0x19, 0, 0xFF, 0, 0, 0xFF, 0x0F, 0})
	if invalid.Cadence != -1 || invalid.Power != -1 {
		t.Errorf("invalid values not detected: %+v", invalid)
	}
}

func TestParseGeneralFE(t *testing.T) {
	// 10 m/s = 10000 = 0x2710, HR 0xFF (none), state ready.
	g := ParseGeneralFE([8]byte{0x10, 0x19, 40, 120, 0x10, 0x27, 0xFF, 0x20})
	if g.SpeedMps != 10 || g.HeartRate != 0 || g.State != StateReady || g.ElapsedQuarterSec != 40 || g.DistanceM != 120 {
		t.Errorf("got %+v", g)
	}
}

func TestControlPages(t *testing.T) {
	if p := BasicResistance(150); p[7] != 200 {
		t.Errorf("resistance not clamped: %d", p[7])
	}
	// 250 W * 4 = 1000 = 0x03E8
	if p := TargetPower(250); p[6] != 0xE8 || p[7] != 0x03 {
		t.Errorf("target power: % X", p)
	}
	// (5 + 200) * 100 = 20500 = 0x5014
	if p := TrackResistance(5); p[5] != 0x14 || p[6] != 0x50 || p[7] != 0xFF {
		t.Errorf("grade: % X", p)
	}
	// (-3 + 200) * 100 = 19700 = 0x4CF4
	if p := TrackResistance(-3); p[5] != 0xF4 || p[6] != 0x4C {
		t.Errorf("negative grade: % X", p)
	}
	// user 75 kg = 7500 = 0x1D4C; bike 9 kg = 180 = 0x0B4; wheel 0.70 m = 70
	p := UserConfig(75, 9, 0.70)
	want := [8]byte{0x37, 0x4C, 0x1D, 0xFF, 0x4F, 0x0B, 70, 0x00}
	if p != want {
		t.Errorf("user config = % X, want % X", p, want)
	}
}
