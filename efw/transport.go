// Package efw is a pure-Go driver for the ZWO EFW (Electronic Filter Wheel),
// talking the device's USB-HID feature-report protocol directly — no cgo SDK.
//
// only the HID transport is platform-specific (IOKit on darwin for dev/test, a
// pure-Go hidraw backend on linux for deployment — to be added).
package efw

// ZWO USB vendor ID and the known EFW product IDs (from the SDK GetProductIDs).
const (
	VID     uint16 = 0x03C3
	PIDEFW1 uint16 = 0x1F01
	PIDEFW2 uint16 = 0x1F02
)

// Transport is the minimal HID feature-report channel the EFW needs. A backend
// (IOKit, hidraw, …) provides it; the EFW logic above is transport-agnostic.
//
// Both calls treat buf[0] as the HID report ID (0x03 for commands, 0x01 for the
// status reply), matching the device's numbered-report convention.
type Transport interface {
	SetFeature(buf []byte) error // write a feature report (buf[0] = report ID)
	GetFeature(buf []byte) error // read a feature report into buf (buf[0] = report ID)
	Close() error
}

// DeviceInfo describes an enumerated/opened HID device.
type DeviceInfo struct {
	PID        uint16
	Serial     string // USB descriptor serial (empty for EFW — use the ZWO serial)
	Product    string
	FeatureLen int    // HID feature report length (observed: 64 or 16)
	LocationID uint32 // stable USB port path; the handle for OpenLocation
}
