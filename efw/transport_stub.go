//go:build !darwin && !linux && !windows

package efw

import "errors"

// On non-darwin platforms there is no HID transport yet (the Linux hidraw backend
// is TODO). These stubs let the package compile and the fake-Transport unit tests
// run in CI with no cgo; the real open/enumerate paths return this error until a
// platform backend exists.
var errNoTransport = errors.New("efw: no HID transport built for this platform (darwin only for now; linux hidraw backend TODO)")

func openFirst() (Transport, DeviceInfo, error)          { return nil, DeviceInfo{}, errNoTransport }
func OpenLocation(uint32) (Transport, DeviceInfo, error) { return nil, DeviceInfo{}, errNoTransport }
func Enumerate() ([]DeviceInfo, error)                   { return nil, errNoTransport }
