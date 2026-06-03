// Package caa is a cgo binding for the ZWO CAA (Camera Angle Adjuster / rotator) SDK.
//
// The ZWO CAA shared library is NOT bundled with this module — install it
// yourself from the ZWO SDK. The header in ./include is enough to compile; the
// library must be resolvable by the linker and at runtime, e.g.:
//
//	install libCAA.{so,dylib} into /usr/local/lib, or
//	CGO_LDFLAGS="-L/path/to/sdk/lib" go build   (build time), and
//	LD_LIBRARY_PATH / DYLD_LIBRARY_PATH=/path/to/sdk/lib   (run time)
//
// Supported targets follow the ZWO SDK: linux (x86/x64/armv6,7,8) and macOS
// (x86_64 and arm64).
package caa

/*
#cgo CFLAGS: -I${SRCDIR}/include -g -Wall
#cgo darwin LDFLAGS: -L/usr/local/lib -lCAA
#cgo linux  LDFLAGS: -L/usr/local/lib -lCAA
#include <stdlib.h>
#include <stdbool.h>
#include <CAA_API.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Error codes returned by the CAA SDK (CAA_ERROR_CODE).
const (
	CAA_SUCCESS = iota
	CAA_ERROR_INVALID_INDEX
	CAA_ERROR_INVALID_ID
	CAA_ERROR_INVALID_VALUE
	CAA_ERROR_REMOVED
	CAA_ERROR_MOVING
	CAA_ERROR_ERROR_STATE
	CAA_ERROR_GENERAL_ERROR
	CAA_ERROR_NOT_SUPPORTED
	CAA_ERROR_CLOSED
	CAA_ERROR_OUT_RANGE
	CAA_ERROR_OVER_LIMIT
	CAA_ERROR_STALL
	CAA_ERROR_TIMEOUT
	CAA_ERROR_INVALID_LENGTH
	CAA_ERROR_END = -1
)

// CaaError wraps a non-success CAA_ERROR_CODE as a Go error.
type CaaError int

func (e CaaError) Error() string {
	return fmt.Sprintf("CAA error code %d", int(e))
}

// errcode converts a C CAA_ERROR_CODE into a Go error (nil on success).
func errcode(code C.CAA_ERROR_CODE) error {
	if int(code) == CAA_SUCCESS {
		return nil
	}
	return CaaError(int(code))
}

// Info mirrors CAA_INFO: the static properties of a rotator.
type Info struct {
	ID      int
	Name    string
	MaxStep int // fixed maximum step count
}

// SDKVersion returns the CAA SDK version string.
func SDKVersion() string {
	return C.GoString(C.CAAGetSDKVersion())
}

// GetNum returns the number of connected rotators. Call this first; it also
// refreshes the device list when rotators are connected or disconnected.
func GetNum() int {
	return int(C.CAAGetNum())
}

// GetID returns the device ID for the rotator at the given enumeration index
// (0..GetNum()-1). All other calls operate on this ID.
func GetID(index int) (int, error) {
	var id C.int
	err := errcode(C.CAAGetID(C.int(index), &id))
	return int(id), err
}

// Open opens the rotator with the given ID. Must be called before use.
func Open(id int) error {
	return errcode(C.CAAOpen(C.int(id)))
}

// Close closes the rotator with the given ID.
func Close(id int) error {
	return errcode(C.CAAClose(C.int(id)))
}

// GetProperty returns the static properties of the rotator.
func GetProperty(id int) (Info, error) {
	var info C.CAA_INFO
	if err := errcode(C.CAAGetProperty(C.int(id), &info)); err != nil {
		return Info{}, err
	}
	return Info{
		ID:      int(info.ID),
		Name:    C.GoString(&info.Name[0]),
		MaxStep: int(info.MaxStep),
	}, nil
}

// Move rotates by a relative angle (degrees). Returns immediately; poll IsMoving.
func Move(id int, angle float32) error {
	return errcode(C.CAAMove(C.int(id), C.float(angle)))
}

// MoveTo rotates to an absolute angle (degrees). Returns immediately; poll IsMoving.
func MoveTo(id int, angle float32) error {
	return errcode(C.CAAMoveTo(C.int(id), C.float(angle)))
}

// MoveToMechanical rotates to an absolute mechanical angle (degrees).
func MoveToMechanical(id int, angle float32) error {
	return errcode(C.CAAMoveToMechanical(C.int(id), C.float(angle)))
}

// Stop halts any motion in progress.
func Stop(id int) error {
	return errcode(C.CAAStop(C.int(id)))
}

// IsMoving reports whether the rotator is moving, and whether it is being driven
// by the hand controller.
func IsMoving(id int) (moving, handControl bool, err error) {
	var mv, hc C.bool
	err = errcode(C.CAAIsMoving(C.int(id), &mv, &hc))
	return bool(mv), bool(hc), err
}

// GetDegree returns the current angle (degrees).
func GetDegree(id int) (float32, error) {
	var deg C.float
	err := errcode(C.CAAGetDegree(C.int(id), &deg))
	return float32(deg), err
}

// SyncDegree sets (syncs) the current angle to the given value without moving
// (CAACurDegree).
func SyncDegree(id int, angle float32) error {
	return errcode(C.CAACurDegree(C.int(id), C.float(angle)))
}

// GetMaxDegree returns the configured maximum angle (degrees).
func GetMaxDegree(id int) (float32, error) {
	var deg C.float
	err := errcode(C.CAAGetMaxDegree(C.int(id), &deg))
	return float32(deg), err
}

// SetMaxDegree sets the maximum angle (degrees).
func SetMaxDegree(id int, angle float32) error {
	return errcode(C.CAASetMaxDegree(C.int(id), C.float(angle)))
}

// GetTemp returns the rotator temperature in degrees Celsius.
func GetTemp(id int) (float32, error) {
	var t C.float
	err := errcode(C.CAAGetTemp(C.int(id), &t))
	return float32(t), err
}

// SetReverse sets whether rotation direction is reversed.
func SetReverse(id int, reversed bool) error {
	return errcode(C.CAASetReverse(C.int(id), C.bool(reversed)))
}

// GetReverse reports whether rotation direction is reversed.
func GetReverse(id int) (bool, error) {
	var r C.bool
	err := errcode(C.CAAGetReverse(C.int(id), &r))
	return bool(r), err
}

// GetFirmwareVersion returns the rotator firmware version (major, minor, build).
func GetFirmwareVersion(id int) (major, minor, build int, err error) {
	var ma, mi, bu C.uchar
	err = errcode(C.CAAGetFirmwareVersion(C.int(id), &ma, &mi, &bu))
	return int(ma), int(mi), int(bu), err
}

// GetSerialNumber returns the rotator's serial number as a hex string.
func GetSerialNumber(id int) (string, error) {
	var sn C.CAA_SN
	if err := errcode(C.CAAGetSerialNumber(C.int(id), &sn)); err != nil {
		return "", err
	}
	b := C.GoBytes(unsafe.Pointer(&sn.id[0]), C.int(len(sn.id)))
	return fmt.Sprintf("%x", b), nil
}
