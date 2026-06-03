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
// NOTE on the header: EAF_focuser.h is a C++ header only because EAFStopAndWait
// declares a default argument (timeoutMs = 1000), which is not valid C. The
// vendored copy in ./include has that one default removed, so the header
// includes cleanly in the cgo C preamble below (the SDK exports everything with
// C linkage). This is the only edit from the ZWO original.
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

#include <EAF_focuser.h>
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
func errcode(code C.EAF_ERROR_CODE) error {
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

// StopAndWait halts motion and blocks until the focuser stops or timeoutMs
// elapses (the blocking counterpart to Stop).
func StopAndWait(id, timeoutMs int) error {
	return errcode(C.EAFStopAndWait(C.int(id), C.int(timeoutMs)))
}

// GetType returns the focuser's model/type string.
func GetType(id int) (string, error) {
	var t C.EAF_TYPE
	if err := errcode(C.EAFGetType(C.int(id), &t)); err != nil {
		return "", err
	}
	return C.GoString(&t._type[0]), nil
}

// SetID sets the focuser's user alias (up to 8 bytes; longer strings are
// truncated). The alias is what GetSerialNumber reads back.
func SetID(id int, alias string) error {
	var a C.EAF_ID
	b := []byte(alias)
	for i := 0; i < len(a.id) && i < len(b); i++ {
		a.id[i] = C.uchar(b[i])
	}
	return errcode(C.EAFSetID(C.int(id), a))
}

// GetLedState reports whether the indicator LED is on.
func GetLedState(id int) (bool, error) {
	var s C.bool
	err := errcode(C.EAFGetLedState(C.int(id), &s))
	return bool(s), err
}

// SetLedState turns the indicator LED on or off.
func SetLedState(id int, on bool) error {
	return errcode(C.EAFSetLedState(C.int(id), C.bool(on)))
}

// SetShippingMode puts the focuser into shipping (battery-storage) mode.
func SetShippingMode(id int) error {
	return errcode(C.EAFSetShippingMode(C.int(id)))
}

// GetReason returns a vendor diagnostic "reason" code.
func GetReason(id int) (int, error) {
	var r C.int
	err := errcode(C.EAFGetReason(C.int(id), &r))
	return int(r), err
}

// GetErrorCode returns the focuser's motor and battery error codes (each a short
// status string).
func GetErrorCode(id int) (motor, battery string, err error) {
	var msg C.EAF_ERROR_MSG
	if e := errcode(C.EAFGetErrorCode(C.int(id), &msg)); e != nil {
		return "", "", e
	}
	return C.GoString(&msg.motor_error_code[0]), C.GoString(&msg.battery_error_code[0]), nil
}

// Check reports whether the USB device with the given VID/PID is an EAF. The SDK
// prefers this over GetProductIDs.
func Check(vid, pid int) bool {
	return C.EAFCheck(C.int(vid), C.int(pid)) == 1
}

// GetProductIDs returns the USB product IDs of supported focusers.
// Deprecated by the SDK in favor of Check.
func GetProductIDs() []int {
	n := int(C.EAFGetProductIDs((*C.int)(nil)))
	if n <= 0 {
		return nil
	}
	buf := make([]C.int, n)
	C.EAFGetProductIDs(&buf[0])
	ids := make([]int, n)
	for i, v := range buf {
		ids[i] = int(v)
	}
	return ids
}

// BatteryInfo mirrors EAF_BATTERY_INFO (units are the SDK's raw values).
type BatteryInfo struct {
	Temp             int
	Voltage          int
	ChargeCurrent    int
	Percentage       int
	DischargeCurrent int
	Health           int
	ChargeVoltage    int
	Cycles           int
}

func toBatteryInfo(bi C.EAF_BATTERY_INFO) BatteryInfo {
	return BatteryInfo{
		Temp:             int(bi.battery_temp),
		Voltage:          int(bi.battery_vol),
		ChargeCurrent:    int(bi.battery_charge_curr),
		Percentage:       int(bi.battery_percentage),
		DischargeCurrent: int(bi.battery_discharge_curr),
		Health:           int(bi.battery_health),
		ChargeVoltage:    int(bi.battery_charge_vol),
		Cycles:           int(bi.battery_num_of_cycles),
	}
}

// GetBatteryInfo returns the focuser's battery telemetry (battery-powered models).
func GetBatteryInfo(id int) (BatteryInfo, error) {
	var bi C.EAF_BATTERY_INFO
	if err := errcode(C.EAFGetBatteryInfo(C.int(id), &bi)); err != nil {
		return BatteryInfo{}, err
	}
	return toBatteryInfo(bi), nil
}

// GetNumOfControls returns how many control-caps entries the focuser exposes.
func GetNumOfControls(id int) (int, error) {
	var n C.int
	err := errcode(C.EAFGetNumOfControls(C.int(id), &n))
	return int(n), err
}

// ControlCaps mirrors EAF_CONTROL_CAPS: metadata for one configurable control.
type ControlCaps struct {
	Name         string
	Description  string
	IsSupported  bool
	IsWritable   bool
	MaxValue     int
	MinValue     int
	DefaultValue int
	ControlType  int // EAF_CONTROL_TYPE
}

// GetControlCaps returns the control at index (0..GetNumOfControls()-1).
func GetControlCaps(id, index int) (ControlCaps, error) {
	var c C.EAF_CONTROL_CAPS
	if err := errcode(C.EAFGetControlCaps(C.int(id), C.int(index), &c)); err != nil {
		return ControlCaps{}, err
	}
	return ControlCaps{
		Name:         C.GoString(&c.name[0]),
		Description:  C.GoString(&c.description[0]),
		IsSupported:  bool(c.isSupported),
		IsWritable:   bool(c.isWritable),
		MaxValue:     int(c.maxValue),
		MinValue:     int(c.minValue),
		DefaultValue: int(c.defaultValue),
		ControlType:  int(c.controlType),
	}, nil
}

// --- Bluetooth (BLE) focusers ------------------------------------------------
//
// The connection-state and pair-state callback registrars
// (EAFBLERegConnStateCallback / EAFBLERegPairStateCallback) are not wrapped —
// they take C function pointers; bridge them with a cgo //export if needed.

// GetBLEName returns the focuser's Bluetooth name.
func GetBLEName(id int) (string, error) {
	var n C.EAF_BLE_NAME
	if err := errcode(C.EAFGetBLEName(C.int(id), &n)); err != nil {
		return "", err
	}
	return C.GoString(&n.name[0]), nil
}

// SetBLEName sets the focuser's Bluetooth name (up to 15 characters).
func SetBLEName(id int, name string) error {
	var n C.EAF_BLE_NAME
	b := []byte(name)
	for i := 0; i < len(n.name)-1 && i < len(b); i++ {
		n.name[i] = C.char(b[i])
	}
	return errcode(C.EAFSetBLEName(C.int(id), n))
}

// BLEDevice mirrors BLE_DEVICE_INFO_T: a scanned Bluetooth focuser.
type BLEDevice struct {
	Name             string
	Address          string
	SignalStrength   int
	BluetoothAddress int64
}

// BLEScan scans for Bluetooth focusers for durationMs, returning up to maxDevices.
func BLEScan(durationMs, maxDevices int) ([]BLEDevice, error) {
	if maxDevices <= 0 {
		return nil, nil
	}
	devs := make([]C.BLE_DEVICE_INFO_T, maxDevices)
	var n C.int
	if err := errcode(C.EAFBLEScan(C.int(durationMs), &devs[0], C.int(maxDevices), &n)); err != nil {
		return nil, err
	}
	out := make([]BLEDevice, int(n))
	for i := range out {
		d := devs[i]
		out[i] = BLEDevice{
			Name:             C.GoString(&d.name[0]),
			Address:          C.GoString(&d.address[0]),
			SignalStrength:   int(d.signalStrength),
			BluetoothAddress: int64(d.bluetoothAddress),
		}
	}
	return out, nil
}

// BLEConnect connects to a Bluetooth focuser by name/address, returning its ID.
func BLEConnect(name, address string) (int, error) {
	cn := C.CString(name)
	defer C.free(unsafe.Pointer(cn))
	ca := C.CString(address)
	defer C.free(unsafe.Pointer(ca))
	var id C.int
	err := errcode(C.EAFBLEConnect(cn, ca, &id))
	return int(id), err
}

// BLEDisconnect disconnects a Bluetooth focuser.
func BLEDisconnect(id int) error { return errcode(C.EAFBLEDisconnect(C.int(id))) }

// BLEPair pairs with a Bluetooth focuser.
func BLEPair(id int) error { return errcode(C.EAFBLEPair(C.int(id))) }

// BLEClearPair clears the Bluetooth pairing.
func BLEClearPair(id int) error { return errcode(C.EAFBLEClearPair(C.int(id))) }

// AllInfo mirrors EAF_ALL_INFO: a consolidated status snapshot (BLE focusers).
type AllInfo struct {
	IsRun            bool
	BacklashSteps    int
	CurrentSteps     int
	Temperature      float32
	BuzzerState      int
	ReverseState     int
	HandlePressed    bool
	HandleConnect    bool
	MaxSteps         int
	LedState         int
	MotorErrorCode   string
	BatteryErrorCode string
	Battery          BatteryInfo
}

// BLEGetAllInfo returns a consolidated status snapshot for a Bluetooth focuser.
func BLEGetAllInfo(id int) (AllInfo, error) {
	var a C.EAF_ALL_INFO
	if err := errcode(C.EAFBLEgetAllInfo(C.int(id), &a)); err != nil {
		return AllInfo{}, err
	}
	return AllInfo{
		IsRun:            a.is_run != 0,
		BacklashSteps:    int(a.backlash_steps),
		CurrentSteps:     int(a.current_steps),
		Temperature:      float32(a.temperature),
		BuzzerState:      int(a.buzzer_state),
		ReverseState:     int(a.reverse_state),
		HandlePressed:    a.handle_pressed != 0,
		HandleConnect:    a.handle_connect != 0,
		MaxSteps:         int(a.max_steps),
		LedState:         int(a.led_state),
		MotorErrorCode:   C.GoStringN(&a.motor_error_code[0], 2),
		BatteryErrorCode: C.GoStringN(&a.battery_error_code[0], 2),
		Battery:          toBatteryInfo(a.battery_info),
	}, nil
}
