// Package eaf is a pure-Go driver for the ZWO EAF (Electronic Auto Focuser),
// talking the device's USB-HID feature-report protocol directly — no cgo SDK.
//
// The EAF is a ZWO HID accessory and shares the EFW's framing (report ID 0x03
// command / 0x01 reply, 7E 5A signature), so this package reuses the same HID
// Transport seam as goasi/efw (IOKit on darwin, hidraw on linux, hid.dll on
// windows).
package eaf

// ZWO USB vendor ID and the EAF product ID (from the device's USB descriptor).
const (
	VID uint16 = 0x03C3
	// The EAF exposes a single product ID; both slots of the shared (EFW-derived)
	// two-PID opener are set to it.
	PIDEAF1 uint16 = 0x1F10
	PIDEAF2 uint16 = 0x1F10
)

// Transport is the minimal HID feature-report channel the EAF needs — identical to
// the EFW's. buf[0] is the HID report ID (0x03 for commands, 0x01 for the reply).
type Transport interface {
	SetFeature(buf []byte) error // write a feature report (buf[0] = report ID)
	GetFeature(buf []byte) error // read a feature report into buf (buf[0] = report ID)
	Close() error
}

// DeviceInfo describes an enumerated/opened HID device.
type DeviceInfo struct {
	PID        uint16
	Serial     string
	Product    string
	FeatureLen int    // HID feature report length (observed: 64 or 16)
	LocationID uint32 // stable USB port path; the handle for OpenLocation
}
