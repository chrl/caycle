// Package fec encodes and decodes ANT+ FE-C (Fitness Equipment Control) data
// pages as tunnelled over Bluetooth LE by Tacx trainers ("FE-C over BLE").
//
// Every BLE packet carries a complete ANT message:
//
//	A4 09 <msgID> 05 <8-byte data page> <checksum>
//
// where the checksum is the XOR of all preceding bytes. The trainer sends
// broadcast messages (0x4E); we send acknowledged messages (0x4F).
package fec

import (
	"errors"
	"fmt"
)

const (
	syncByte       = 0xA4
	payloadLen     = 0x09 // channel byte + 8 data bytes
	msgBroadcast   = 0x4E
	msgAcknowledge = 0x4F
	channel        = 0x05
	frameLen       = 13
)

// Data page numbers.
const (
	PageGeneralFE       = 0x10
	PageTrainerData     = 0x19
	PageBasicResistance = 0x30
	PageTargetPower     = 0x31
	PageTrackResistance = 0x33
	PageUserConfig      = 0x37
)

// FEState is the fitness equipment state reported in pages 0x10 and 0x19.
type FEState uint8

const (
	StateReserved FEState = 0
	StateAsleep   FEState = 1
	StateReady    FEState = 2
	StateInUse    FEState = 3
	StateFinished FEState = 4
)

func (s FEState) String() string {
	switch s {
	case StateAsleep:
		return "asleep"
	case StateReady:
		return "ready"
	case StateInUse:
		return "in use"
	case StateFinished:
		return "paused"
	default:
		return "unknown"
	}
}

// TargetPowerStatus tells whether the trainer can reach the ERG target.
type TargetPowerStatus uint8

const (
	TargetOK        TargetPowerStatus = 0
	TargetSpeedLow  TargetPowerStatus = 1
	TargetSpeedHigh TargetPowerStatus = 2
	TargetUnknown   TargetPowerStatus = 3
)

func (s TargetPowerStatus) String() string {
	switch s {
	case TargetOK:
		return "ok"
	case TargetSpeedLow:
		return "speed too low, pedal faster"
	case TargetSpeedHigh:
		return "speed too high, shift down"
	default:
		return "undetermined"
	}
}

// GeneralFE is data page 0x10.
type GeneralFE struct {
	ElapsedQuarterSec uint8   // rolls over every 64 s
	DistanceM         uint8   // rolls over every 256 m
	SpeedMps          float64 // instantaneous speed
	HeartRate         int     // bpm, 0 if not available
	State             FEState
}

// TrainerData is data page 0x19.
type TrainerData struct {
	EventCount       uint8
	Cadence          int    // rpm, -1 if not available
	AccumulatedPower uint16 // W, rolls over at 65536
	Power            int    // W, -1 if not available
	PowerCalibration bool   // trainer asks for a power calibration
	ResistanceCalib  bool   // trainer asks for a resistance calibration
	UserConfigNeeded bool   // trainer asks for user configuration (page 0x37)
	TargetStatus     TargetPowerStatus
	State            FEState
}

var (
	ErrShortFrame = errors.New("fec: frame too short")
	ErrBadSync    = errors.New("fec: bad sync byte")
	ErrChecksum   = errors.New("fec: checksum mismatch")
)

func checksum(b []byte) byte {
	var c byte
	for _, x := range b {
		c ^= x
	}
	return c
}

// Encode wraps an 8-byte data page into an acknowledged ANT message ready to
// be written to the trainer's FE-C write characteristic.
func Encode(page [8]byte) []byte {
	out := make([]byte, 0, frameLen)
	out = append(out, syncByte, payloadLen, msgAcknowledge, channel)
	out = append(out, page[:]...)
	return append(out, checksum(out))
}

// Decode extracts the 8-byte data page from a single ANT message.
func Decode(frame []byte) ([8]byte, error) {
	var page [8]byte
	if len(frame) < frameLen {
		return page, ErrShortFrame
	}
	if frame[0] != syncByte {
		return page, ErrBadSync
	}
	if frame[1] != payloadLen {
		return page, fmt.Errorf("fec: unexpected payload length %d", frame[1])
	}
	if checksum(frame[:frameLen-1]) != frame[frameLen-1] {
		return page, ErrChecksum
	}
	copy(page[:], frame[4:12])
	return page, nil
}

// ParseGeneralFE decodes data page 0x10.
func ParseGeneralFE(p [8]byte) GeneralFE {
	g := GeneralFE{
		ElapsedQuarterSec: p[2],
		DistanceM:         p[3],
		SpeedMps:          float64(uint16(p[4])|uint16(p[5])<<8) / 1000,
		State:             FEState(p[7] >> 4),
	}
	if p[6] != 0xFF {
		g.HeartRate = int(p[6])
	}
	return g
}

// ParseTrainerData decodes data page 0x19.
func ParseTrainerData(p [8]byte) TrainerData {
	t := TrainerData{
		EventCount:       p[1],
		Cadence:          int(p[2]),
		AccumulatedPower: uint16(p[3]) | uint16(p[4])<<8,
		Power:            int(uint16(p[5]) | uint16(p[6]&0x0F)<<8),
		PowerCalibration: p[6]&0x10 != 0,
		ResistanceCalib:  p[6]&0x20 != 0,
		UserConfigNeeded: p[6]&0x40 != 0,
		TargetStatus:     TargetPowerStatus(p[7] & 0x03),
		State:            FEState(p[7] >> 4),
	}
	if p[2] == 0xFF {
		t.Cadence = -1
	}
	if t.Power == 0xFFF {
		t.Power = -1
	}
	return t
}

func clamp(v, lo, hi float64) float64 {
	return max(lo, min(hi, v))
}

// BasicResistance builds page 0x30: resistance as a percentage (0-100) of the
// trainer's maximum, in 0.5 % steps.
func BasicResistance(percent float64) [8]byte {
	v := byte(clamp(percent, 0, 100)*2 + 0.5)
	return [8]byte{PageBasicResistance, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, v}
}

// TargetPower builds page 0x31 (ERG mode): target power in watts, 0.25 W steps.
func TargetPower(watts float64) [8]byte {
	v := uint16(clamp(watts, 0, 4000)*4 + 0.5)
	return [8]byte{PageTargetPower, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, byte(v), byte(v >> 8)}
}

// TrackResistance builds page 0x33 (simulation mode): road grade in percent
// (-200..200, 0.01 % steps). The rolling resistance coefficient is left at
// the trainer default.
func TrackResistance(gradePercent float64) [8]byte {
	v := uint16((clamp(gradePercent, -200, 200)+200)*100 + 0.5)
	return [8]byte{PageTrackResistance, 0xFF, 0xFF, 0xFF, 0xFF, byte(v), byte(v >> 8), 0xFF}
}

// UserConfig builds page 0x37: rider and bike weight (kg) and wheel diameter
// (m). The trainer uses these in simulation mode.
func UserConfig(userKg, bikeKg, wheelM float64) [8]byte {
	user := uint16(clamp(userKg, 0, 655.34)*100 + 0.5)
	bike := uint16(clamp(bikeKg, 0, 50)*20+0.5) & 0x0FFF
	wheel := byte(clamp(wheelM, 0, 2.54)*100 + 0.5)
	return [8]byte{
		PageUserConfig,
		byte(user), byte(user >> 8),
		0xFF,
		0x0F | byte(bike&0x0F)<<4, // wheel diameter offset: not set
		byte(bike >> 4),
		wheel,
		0x00, // gear ratio: not set
	}
}
