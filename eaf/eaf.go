// Package eaf is a cgo binding for the ZWO EAF (Electronic Auto Focuser) SDK.
//
// The ZWO EAFFocuser shared library is NOT bundled with this module — install it
// yourself from the ZWO SDK. The library must be resolvable by the linker and at
// runtime, e.g.:
//
//	install libEAFFocuser.{so,dylib} into /usr/local/lib, or
//	CGO_LDFLAGS="-L/path/to/sdk/lib" go build   (build time), and
//	LD_LIBRARY_PATH / DYLD_LIBRARY_PATH=/path/to/sdk/lib   (run time)
//
// On Linux the SDK additionally requires libsdbus-c++.so.2 and libWrapperSdbus.so
// from the same lib directory.
//
// NOTE on the header: EAF_focuser.h (kept in ./include for reference) is a C++
// header — EAFStopAndWait declares a default argument, which is not valid C — so
// it cannot be included in a cgo C preamble. Instead the C-ABI prototypes and the
// EAF_INFO layout are declared directly below; the ZWO library exports these with
// C linkage (extern "C"). Keep these declarations in sync with the header.
//
// Supported targets follow the ZWO SDK: linux (x86/x64/armv6,7,8) and macOS
// (x86_64 and arm64).
package eaf

/*
#cgo CFLAGS: -I${SRCDIR}/include -g -Wall
#cgo darwin LDFLAGS: -L/usr/local/lib -lEAFFocuser
#cgo linux  LDFLAGS: -L/usr/local/lib -lEAFFocuser
#include <stdlib.h>
#include <stdbool.h>

typedef struct { int ID; char Name[64]; int MaxStep; } EAF_INFO;
typedef struct { unsigned char id[8]; } EAF_SN;

extern int         EAFGetNum(void);
extern int         EAFGetID(int index, int* ID);
extern int         EAFOpen(int ID);
extern int         EAFClose(int ID);
extern int         EAFGetProperty(int ID, EAF_INFO* pInfo);
extern int         EAFMove(int ID, int iStep);
extern int         EAFStop(int ID);
extern int         EAFIsMoving(int ID, bool* pbVal, bool* pbHandControl);
extern int         EAFGetPosition(int ID, int* piStep);
extern int         EAFResetPostion(int ID, int iStep);
extern int         EAFGetTemp(int ID, float* pfTemp);
extern int         EAFSetMaxStep(int ID, int iVal);
extern int         EAFGetMaxStep(int ID, int* piVal);
extern int         EAFStepRange(int ID, int* piVal);
extern int         EAFSetReverse(int ID, bool bVal);
extern int         EAFGetReverse(int ID, bool* pbVal);
extern int         EAFSetBeep(int ID, bool bVal);
extern int         EAFGetBeep(int ID, bool* pbVal);
extern int         EAFSetBacklash(int ID, int iVal);
extern int         EAFGetBacklash(int ID, int* piVal);
extern const char* EAFGetSDKVersion(void);
extern int         EAFGetFirmwareVersion(int ID, unsigned char* major, unsigned char* minor, unsigned char* build);
extern int         EAFGetSerialNumber(int ID, EAF_SN* pSN);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Error codes returned by the EAF SDK (EAF_ERROR_CODE). BLE-specific codes
// (>= 50) are omitted; this binding covers USB-connected focusers.
const (
	EAF_SUCCESS = iota
	EAF_ERROR_INVALID_INDEX
	EAF_ERROR_INVALID_ID
	EAF_ERROR_INVALID_VALUE
	EAF_ERROR_REMOVED
	EAF_ERROR_MOVING
	EAF_ERROR_ERROR_STATE
	EAF_ERROR_GENERAL_ERROR
	EAF_ERROR_NOT_SUPPORTED
	EAF_ERROR_CLOSED
	EAF_ERROR_BATTER_INFO
	EAF_ERROR_INVALID_LENGTH
	EAF_ERROR_END = -1
)

// EafError wraps a non-success EAF_ERROR_CODE as a Go error.
type EafError int

func (e EafError) Error() string {
	return fmt.Sprintf("EAF error code %d", int(e))
}

// errcode converts a C EAF_ERROR_CODE into a Go error (nil on success).
func errcode(code C.int) error {
	if int(code) == EAF_SUCCESS {
		return nil
	}
	return EafError(int(code))
}

// Info mirrors EAF_INFO: the static properties of a focuser.
type Info struct {
	ID      int
	Name    string
	MaxStep int // fixed maximum position
}

// SDKVersion returns the EAF SDK version string.
func SDKVersion() string {
	return C.GoString(C.EAFGetSDKVersion())
}

// GetNum returns the number of connected focusers. Call this first; it also
// refreshes the device list when focusers are connected or disconnected.
func GetNum() int {
	return int(C.EAFGetNum())
}

// GetID returns the device ID for the focuser at the given enumeration index
// (0..GetNum()-1). All other calls operate on this ID.
func GetID(index int) (int, error) {
	var id C.int
	err := errcode(C.EAFGetID(C.int(index), &id))
	return int(id), err
}

// Open opens the focuser with the given ID. Must be called before use.
func Open(id int) error {
	return errcode(C.EAFOpen(C.int(id)))
}

// Close closes the focuser with the given ID.
func Close(id int) error {
	return errcode(C.EAFClose(C.int(id)))
}

// GetProperty returns the static properties of the focuser.
func GetProperty(id int) (Info, error) {
	var info C.EAF_INFO
	if err := errcode(C.EAFGetProperty(C.int(id), &info)); err != nil {
		return Info{}, err
	}
	return Info{
		ID:      int(info.ID),
		Name:    C.GoString(&info.Name[0]),
		MaxStep: int(info.MaxStep),
	}, nil
}

// Move moves the focuser to the absolute step position. Returns immediately;
// poll IsMoving / GetPosition to detect completion.
func Move(id, step int) error {
	return errcode(C.EAFMove(C.int(id), C.int(step)))
}

// Stop halts any motion in progress.
func Stop(id int) error {
	return errcode(C.EAFStop(C.int(id)))
}

// IsMoving reports whether the focuser is moving, and whether it is being driven
// by the hand controller.
func IsMoving(id int) (moving, handControl bool, err error) {
	var mv, hc C.bool
	err = errcode(C.EAFIsMoving(C.int(id), &mv, &hc))
	return bool(mv), bool(hc), err
}

// GetPosition returns the current step position.
func GetPosition(id int) (int, error) {
	var step C.int
	err := errcode(C.EAFGetPosition(C.int(id), &step))
	return int(step), err
}

// ResetPosition redefines the current position to be the given step value
// without moving (EAFResetPostion).
func ResetPosition(id, step int) error {
	return errcode(C.EAFResetPostion(C.int(id), C.int(step)))
}

// GetTemp returns the focuser temperature in degrees Celsius.
func GetTemp(id int) (float32, error) {
	var t C.float
	err := errcode(C.EAFGetTemp(C.int(id), &t))
	return float32(t), err
}

// SetMaxStep sets the maximum step position.
func SetMaxStep(id, max int) error {
	return errcode(C.EAFSetMaxStep(C.int(id), C.int(max)))
}

// GetMaxStep returns the maximum step position.
func GetMaxStep(id int) (int, error) {
	var v C.int
	err := errcode(C.EAFGetMaxStep(C.int(id), &v))
	return int(v), err
}

// StepRange returns the hardware step range of the focuser.
func StepRange(id int) (int, error) {
	var v C.int
	err := errcode(C.EAFStepRange(C.int(id), &v))
	return int(v), err
}

// SetReverse sets whether the movement direction is reversed.
func SetReverse(id int, reversed bool) error {
	return errcode(C.EAFSetReverse(C.int(id), C.bool(reversed)))
}

// GetReverse reports whether the movement direction is reversed.
func GetReverse(id int) (bool, error) {
	var r C.bool
	err := errcode(C.EAFGetReverse(C.int(id), &r))
	return bool(r), err
}

// SetBeep sets whether the focuser beeps.
func SetBeep(id int, on bool) error {
	return errcode(C.EAFSetBeep(C.int(id), C.bool(on)))
}

// GetBeep reports whether the focuser beep is enabled.
func GetBeep(id int) (bool, error) {
	var b C.bool
	err := errcode(C.EAFGetBeep(C.int(id), &b))
	return bool(b), err
}

// SetBacklash sets the backlash compensation in steps.
func SetBacklash(id, steps int) error {
	return errcode(C.EAFSetBacklash(C.int(id), C.int(steps)))
}

// GetBacklash returns the backlash compensation in steps.
func GetBacklash(id int) (int, error) {
	var v C.int
	err := errcode(C.EAFGetBacklash(C.int(id), &v))
	return int(v), err
}

// GetFirmwareVersion returns the focuser firmware version (major, minor, build).
func GetFirmwareVersion(id int) (major, minor, build int, err error) {
	var ma, mi, bu C.uchar
	err = errcode(C.EAFGetFirmwareVersion(C.int(id), &ma, &mi, &bu))
	return int(ma), int(mi), int(bu), err
}

// GetSerialNumber returns the focuser's serial number as a hex string.
func GetSerialNumber(id int) (string, error) {
	var sn C.EAF_SN
	if err := errcode(C.EAFGetSerialNumber(C.int(id), &sn)); err != nil {
		return "", err
	}
	b := C.GoBytes(unsafe.Pointer(&sn.id[0]), C.int(len(sn.id)))
	return fmt.Sprintf("%x", b), nil
}
