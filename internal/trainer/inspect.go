package trainer

import (
	"fmt"
	"io"

	"tinygo.org/x/bluetooth"
)

var knownUUIDs = map[bluetooth.UUID]string{
	FECService:                     "Tacx FE-C over BLE",
	fecNotify:                      "FE-C data (notify)",
	fecWrite:                       "FE-C control (write)",
	FTMSService:                    "Fitness Machine (FTMS)",
	CyclingPowerService:            "Cycling Power",
	CyclingSpeedCadence:            "Cycling Speed and Cadence",
	bluetooth.New16BitUUID(0x180A): "Device Information",
	bluetooth.New16BitUUID(0x180F): "Battery",
	bluetooth.New16BitUUID(0x1800): "Generic Access",
	bluetooth.New16BitUUID(0x1801): "Generic Attribute",
	bluetooth.New16BitUUID(0x2A63): "Cycling Power Measurement",
	bluetooth.New16BitUUID(0x2A5B): "CSC Measurement",
	bluetooth.New16BitUUID(0x2AD2): "Indoor Bike Data",
	bluetooth.New16BitUUID(0x2AD9): "Fitness Machine Control Point",
	bluetooth.New16BitUUID(0x2A29): "Manufacturer Name",
	bluetooth.New16BitUUID(0x2A24): "Model Number",
	bluetooth.New16BitUUID(0x2A25): "Serial Number",
	bluetooth.New16BitUUID(0x2A26): "Firmware Revision",
	bluetooth.New16BitUUID(0x2A27): "Hardware Revision",
	bluetooth.New16BitUUID(0x2A28): "Software Revision",
}

// Inspect connects to a device and prints its GATT services and characteristics.
func Inspect(adapter *bluetooth.Adapter, r bluetooth.ScanResult, w io.Writer) error {
	dev, err := adapter.Connect(r.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer dev.Disconnect()

	svcs, err := dev.DiscoverServices(nil)
	if err != nil {
		return fmt.Errorf("discover services: %w", err)
	}
	for _, svc := range svcs {
		fmt.Fprintf(w, "service %s  %s\n", svc.UUID(), knownUUIDs[svc.UUID()])
		chars, err := svc.DiscoverCharacteristics(nil)
		if err != nil {
			fmt.Fprintf(w, "  (characteristics: %v)\n", err)
			continue
		}
		for _, c := range chars {
			fmt.Fprintf(w, "  char %s  %s\n", c.UUID(), knownUUIDs[c.UUID()])
		}
	}
	return nil
}
