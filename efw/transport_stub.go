//go:build !darwin && !linux && !windows

package efw

import "errors"

// darwin, linux, and windows each have a real HID transport; this stub covers any
// other platform. It lets the package compile and the fake-Transport unit tests
// run with no cgo, while the real open/enumerate paths return this error.
var errNoTransport = errors.New("efw: no HID transport built for this platform (supported: darwin, linux, windows)")

func openFirst() (Transport, DeviceInfo, error)          { return nil, DeviceInfo{}, errNoTransport }
func OpenLocation(uint32) (Transport, DeviceInfo, error) { return nil, DeviceInfo{}, errNoTransport }
func Enumerate() ([]DeviceInfo, error)                   { return nil, errNoTransport }
