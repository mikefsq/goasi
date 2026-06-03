// Package efw is a cgo binding for the ZWO EFW (Electronic Filter Wheel) SDK.
//
// The ZWO EFWFilter shared library is NOT bundled with this module — install it
// yourself from the ZWO SDK. The header in ./include is enough to compile; the
// library must be resolvable by the linker and at runtime, e.g.:
//
//	install libEFWFilter.{so,dylib} into /usr/local/lib, or
//	CGO_LDFLAGS="-L/path/to/sdk/lib" go build   (build time), and
//	LD_LIBRARY_PATH / DYLD_LIBRARY_PATH=/path/to/sdk/lib   (run time)
//
// Supported targets follow the ZWO SDK: linux (x86/x64/armv6,7,8) and macOS
// (x86_64 and arm64).
package efw

/*
#cgo CFLAGS: -I${SRCDIR}/include -g -Wall
#cgo darwin LDFLAGS: -L/usr/local/lib -lEFWFilter
#cgo linux  LDFLAGS: -L/usr/local/lib -lEFWFilter
#include <stdlib.h>
#include <stdbool.h>
#include <EFW_filter.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Error codes returned by the EFW SDK (EFW_ERROR_CODE).
const (
	EFW_SUCCESS = iota
	EFW_ERROR_INVALID_INDEX
	EFW_ERROR_INVALID_ID
	EFW_ERROR_INVALID_VALUE
	EFW_ERROR_REMOVED
	EFW_ERROR_MOVING
	EFW_ERROR_ERROR_STATE
	EFW_ERROR_GENERAL_ERROR
	EFW_ERROR_NOT_SUPPORTED
	EFW_ERROR_INVALID_LENGTH
	EFW_ERROR_CLOSED
	EFW_ERROR_END = -1
)

// EfwError wraps a non-success EFW_ERROR_CODE as a Go error.
type EfwError int

func (e EfwError) Error() string {
	return fmt.Sprintf("EFW error code %d", int(e))
}

// errcode converts a C EFW_ERROR_CODE into a Go error (nil on success).
func errcode(code C.EFW_ERROR_CODE) error {
	if int(code) == EFW_SUCCESS {
		return nil
	}
	return EfwError(int(code))
}

// Info mirrors EFW_INFO: the static properties of a filter wheel.
type Info struct {
	ID      int
	Name    string
	SlotNum int // number of filter slots
}

// SDKVersion returns the EFW SDK version string.
func SDKVersion() string {
	return C.GoString(C.EFWGetSDKVersion())
}

// GetNum returns the number of connected filter wheels. Call this first; it also
// refreshes the device list when wheels are connected or disconnected.
func GetNum() int {
	return int(C.EFWGetNum())
}

// GetID returns the device ID for the wheel at the given enumeration index
// (0..GetNum()-1). All other calls operate on this ID.
func GetID(index int) (int, error) {
	var id C.int
	err := errcode(C.EFWGetID(C.int(index), &id))
	return int(id), err
}

// Open opens the filter wheel with the given ID. Must be called before use.
func Open(id int) error {
	return errcode(C.EFWOpen(C.int(id)))
}

// Close closes the filter wheel with the given ID.
func Close(id int) error {
	return errcode(C.EFWClose(C.int(id)))
}

// GetProperty returns the static properties of the wheel. SlotNum is 0 until the
// wheel has finished detecting its slot count after connection.
func GetProperty(id int) (Info, error) {
	var info C.EFW_INFO
	if err := errcode(C.EFWGetProperty(C.int(id), &info)); err != nil {
		return Info{}, err
	}
	return Info{
		ID:      int(info.ID),
		Name:    C.GoString(&info.Name[0]),
		SlotNum: int(info.slotNum),
	}, nil
}

// GetPosition returns the current slot position (0-based). -1 means the wheel is
// still moving / position is unknown.
func GetPosition(id int) (int, error) {
	var pos C.int
	err := errcode(C.EFWGetPosition(C.int(id), &pos))
	return int(pos), err
}

// SetPosition moves the wheel to the given slot position (0-based). This returns
// immediately; poll GetPosition to detect completion.
func SetPosition(id, position int) error {
	return errcode(C.EFWSetPosition(C.int(id), C.int(position)))
}

// SetDirection sets whether the wheel only turns one direction (unidirectional).
func SetDirection(id int, unidirectional bool) error {
	return errcode(C.EFWSetDirection(C.int(id), C.bool(unidirectional)))
}

// GetDirection reports whether the wheel is configured as unidirectional.
func GetDirection(id int) (bool, error) {
	var uni C.bool
	err := errcode(C.EFWGetDirection(C.int(id), &uni))
	return bool(uni), err
}

// Calibrate runs the wheel's calibration routine.
func Calibrate(id int) error {
	return errcode(C.EFWCalibrate(C.int(id)))
}

// GetFirmwareVersion returns the wheel's firmware version (major, minor, build).
func GetFirmwareVersion(id int) (major, minor, build int, err error) {
	var ma, mi, bu C.uchar
	err = errcode(C.EFWGetFirmwareVersion(C.int(id), &ma, &mi, &bu))
	return int(ma), int(mi), int(bu), err
}

// GetSerialNumber returns the wheel's serial number as a hex string.
func GetSerialNumber(id int) (string, error) {
	var sn C.EFW_SN
	if err := errcode(C.EFWGetSerialNumber(C.int(id), &sn)); err != nil {
		return "", err
	}
	b := C.GoBytes(unsafe.Pointer(&sn.id[0]), C.int(len(sn.id)))
	return fmt.Sprintf("%x", b), nil
}

// SetID sets the wheel's user alias (up to 8 bytes; longer strings are
// truncated). The alias is what GetSerialNumber reads back.
func SetID(id int, alias string) error {
	var a C.EFW_ID
	b := []byte(alias)
	for i := 0; i < len(a.id) && i < len(b); i++ {
		a.id[i] = C.uchar(b[i])
	}
	return errcode(C.EFWSetID(C.int(id), a))
}

// GetHWErrorCode returns the wheel's hardware error code (0 = no error).
func GetHWErrorCode(id int) (int, error) {
	var code C.int
	err := errcode(C.EFWGetHWErrorCode(C.int(id), &code))
	return int(code), err
}

// Check reports whether the USB device with the given VID/PID is an EFW (the EFW
// VID is 0x03C3). The SDK prefers this over GetProductIDs.
func Check(vid, pid int) bool {
	return C.EFWCheck(C.int(vid), C.int(pid)) == 1
}

// GetProductIDs returns the USB product IDs of supported filter wheels.
// Deprecated by the SDK in favor of Check.
func GetProductIDs() []int {
	n := int(C.EFWGetProductIDs((*C.int)(nil)))
	if n <= 0 {
		return nil
	}
	buf := make([]C.int, n)
	C.EFWGetProductIDs(&buf[0])
	ids := make([]int, n)
	for i, v := range buf {
		ids[i] = int(v)
	}
	return ids
}
