// Package ble holds Bluetooth helpers shared by the trainer and heart rate
// monitor connections.
package ble

import (
	"fmt"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// Find scans until match returns true for a device or the timeout expires.
//
// Only one scan can run at a time, so concurrent callers take turns.
func Find(adapter *bluetooth.Adapter, timeout time.Duration, match func(bluetooth.ScanResult) bool) (bluetooth.ScanResult, error) {
	scanMu.Lock()
	defer scanMu.Unlock()
	var (
		found bluetooth.ScanResult
		ok    bool
	)
	timer := time.AfterFunc(timeout, func() { adapter.StopScan() })
	defer timer.Stop()
	err := adapter.Scan(func(a *bluetooth.Adapter, r bluetooth.ScanResult) {
		if !ok && match(r) {
			found, ok = r, true
			a.StopScan()
		}
	})
	if err != nil {
		return found, err
	}
	if !ok {
		return found, fmt.Errorf("no matching device found within %s", timeout)
	}
	return found, nil
}

var scanMu sync.Mutex

var (
	handlerOnce sync.Once
	mu          sync.Mutex
	onLost      = map[string]func(){}
)

// OnDisconnect registers f to run once when the device at addr disconnects.
// The adapter has a single connect handler, so all connections share this one.
func OnDisconnect(adapter *bluetooth.Adapter, addr bluetooth.Address, f func()) {
	handlerOnce.Do(func() {
		adapter.SetConnectHandler(func(d bluetooth.Device, connected bool) {
			if connected {
				return
			}
			mu.Lock()
			cb := onLost[d.Address.String()]
			delete(onLost, d.Address.String())
			mu.Unlock()
			if cb != nil {
				cb()
			}
		})
	})
	mu.Lock()
	onLost[addr.String()] = f
	mu.Unlock()
}
