package efw

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// --- ZWO HID framing (kept inline; extract a shared package only once a 2nd
// HID device exists — rule of three). All ZWO accessories share this shape:
//
//	command : [0]=0x03 reportID  [1]=0x7E [2]=0x5A sig  [3]=opcode  [4..]=args
//	reply   : [0]=0x01 reportID  [4..]=status fields
const (
	repIDCmd   = 0x03
	repIDReply = 0x01
	sig0       = 0x7E
	sig1       = 0x5A

	opMotion     = 0x01 // motion; byte[4] selects the sub-function (below)
	opQuery      = 0x02 // query family; byte[4] selects the sub-function (below)
	opWriteAlias = 0x0D // [03 7E 5A 0D <≤8 alias bytes>] — persistent flash write

	aliasLen = 8 // user alias is 8 bytes (matches EFW_ID / the serial readback)

	// opMotion sub-functions (byte[4]).
	motCalibrate = 0x01 // home + re-align slots; no target, no reply
	motClearErr  = 0x0F // clear a latched error state; no reply
	// 0x02 / 0x03 are bidirectional / unidirectional move (see dir* below).

	// opQuery subcodes (byte[4]), confirmed from the SDK's constant table @0x7940.
	subStatus = 0x01 // -> position/state report
	subInfo   = 0x04 // -> open handshake: firmware/model
	subSerial = 0x0C // -> factory serial number (read-only)
	subAlias  = 0x0D // -> user alias (read-only; 0x0D is also the write opcode)

	// Move direction (byte[4]). The SDK keeps this as a host-side flag
	// (EFWSetDirection just sets it; nothing is sent to the wheel) and stamps it
	// into each move command. Bidirectional = firmware picks the shortest arc;
	// unidirectional = firmware always rotates the same way, so every slot is
	// approached from the same side and gear backlash is taken up identically
	// (repeatable seating, at the cost of speed). These are opMotion byte[4]
	// values, alongside motCalibrate.
	dirBidirectional  = 0x02
	dirUnidirectional = 0x03
)

// moveSettle is the post-write settle the SDK applies after a move/calibrate/
// alias-write (the MCU accepting the command, not the physical rotation — that is
// observed asynchronously via Position). A var so tests can zero it.
var moveSettle = 200 * time.Millisecond

// Status-report field offsets, confirmed against hardware (a move from slot 1→4
// showed byte4=state, byte6=target, byte7=current, all 1-based on the wire;
// byte9 constant = slot count).
const (
	statusByteState  = 4 // 0x01 idle, 0x04 moving
	statusByteTarget = 6 // commanded slot, 1-based
	statusBytePos    = 7 // current slot, 1-based on the wire
	statusByteSlots  = 9 // number of slots (constant)

	stateCalibrating = 0x00
	stateIdle        = 0x01
	stateMoving      = 0x04
	stateError       = 0x06
)

// EFW is an opened filter wheel.
type EFW struct {
	t          Transport
	info       DeviceInfo
	featureLen int

	mu             sync.Mutex // serializes a command + its reply (per-device, like the SDK)
	unidirectional bool       // host-side; stamped into each move command's byte[4]
}

// SetUnidirectional selects unidirectional (true) or bidirectional (false) moves.
// Host-side only — it takes effect on the next SetPosition; nothing is sent now.
func (e *EFW) SetUnidirectional(on bool) {
	e.mu.Lock()
	e.unidirectional = on
	e.mu.Unlock()
}

// Unidirectional reports the current move-direction mode.
func (e *EFW) Unidirectional() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.unidirectional
}

// newEFW wraps an opened transport, normalizing the feature length.
func newEFW(t Transport, info DeviceInfo) *EFW {
	fl := info.FeatureLen
	if fl != 64 && fl != 16 {
		fl = 64 // descriptor reported something odd; the protocol uses 64 or 16
	}
	info.FeatureLen = fl
	return &EFW{t: t, info: info, featureLen: fl}
}

// OpenFirst finds and opens the first attached ZWO EFW.
func OpenFirst() (*EFW, error) {
	t, info, err := openFirst()
	if err != nil {
		return nil, err
	}
	return newEFW(t, info), nil
}

// OpenAt opens the EFW at a specific USB locationID (from Enumerate / List).
func OpenAt(locationID uint32) (*EFW, error) {
	t, info, err := OpenLocation(locationID)
	if err != nil {
		return nil, err
	}
	return newEFW(t, info), nil
}

// Listing is one entry from List: an enumerated EFW plus its queried identity.
type Listing struct {
	LocationID uint32
	PID        uint16
	Serial     string // ZWO factory serial (hex); empty if the device couldn't be opened
	Slots      int
}

// List enumerates every attached EFW and, for each, briefly opens it to read the
// ZWO factory serial and slot count (the serial is not in the USB layer). Devices
// that can't be opened (e.g. held by another process) are returned with an empty
// Serial.
func List() ([]Listing, error) {
	devs, err := Enumerate()
	if err != nil {
		return nil, err
	}
	out := make([]Listing, 0, len(devs))
	for _, d := range devs {
		l := Listing{LocationID: d.LocationID, PID: d.PID}
		if e, err := OpenAt(d.LocationID); err == nil {
			if s, err := e.SerialZWO(); err == nil {
				l.Serial = s // canonical ZWO form, matches ZWO/ASCOM tooling
			}
			l.Slots = e.Slots()
			e.Close()
		}
		out = append(out, l)
	}
	return out, nil
}

// OpenBySerial opens the EFW whose ZWO factory serial matches (case-insensitive
// hex). This is the stable, multi-device-safe way to bind a wheel — unlike the
// enumeration index, the serial follows the physical unit (audit F4).
func OpenBySerial(serial string) (*EFW, error) {
	list, err := List()
	if err != nil {
		return nil, err
	}
	for _, l := range list {
		if l.Serial != "" && strings.EqualFold(l.Serial, serial) {
			return OpenAt(l.LocationID)
		}
	}
	return nil, fmt.Errorf("no EFW with serial %q (found %d wheel(s))", serial, len(list))
}

func (e *EFW) Info() DeviceInfo { return e.info }
func (e *EFW) FeatureLen() int  { return e.featureLen }

func (e *EFW) Close() error { return e.t.Close() }

// RawStatus issues a status query and returns the raw reply report, for
// validation/debugging against the real hardware.
func (e *EFW) RawStatus() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.statusLocked()
}

// statusLocked sends [03 7E 5A 02 01] then reads the report-ID-1 reply. No
// settle (mirrors the SDK clearError path). Caller holds mu.
func (e *EFW) statusLocked() ([]byte, error) {
	q := make([]byte, e.featureLen)
	q[0], q[1], q[2], q[3], q[4] = repIDCmd, sig0, sig1, opQuery, subStatus
	if err := e.t.SetFeature(q); err != nil {
		return nil, fmt.Errorf("status query: %w", err)
	}
	r := make([]byte, e.featureLen)
	r[0] = repIDReply
	if err := e.t.GetFeature(r); err != nil {
		return nil, fmt.Errorf("status read: %w", err)
	}
	return r, nil
}

// Serial reads the device's factory serial via the [03 7E 5A 02 0C] query and
// returns the raw reply payload (reply[4:]) plus its hex string. Read-only.
// (The SDK additionally nibble-unpacks this into its own format; raw is enough
// to tell whether the device carries a unique, non-zero serial.)
func (e *EFW) Serial() (raw []byte, hexStr string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	q := make([]byte, e.featureLen)
	q[0], q[1], q[2], q[3], q[4] = repIDCmd, sig0, sig1, opQuery, subSerial
	if err := e.t.SetFeature(q); err != nil {
		return nil, "", fmt.Errorf("serial query: %w", err)
	}
	r := make([]byte, e.featureLen)
	r[0] = repIDReply
	if err := e.t.GetFeature(r); err != nil {
		return nil, "", fmt.Errorf("serial read: %w", err)
	}
	end := 16
	if len(r) < end {
		end = len(r)
	}
	raw = append([]byte(nil), r[4:end]...)
	return raw, hex.EncodeToString(raw), nil
}

// SerialZWO returns the factory serial formatted exactly as ZWO's
// EFWGetSerialNumber does (16 hex chars), so it matches ZWO/ASCOM tooling. The
// device sends a packed form; the SDK unpacks it as a 16-nibble stream:
// out[0..2] are nibble pairs of raw[0..5]; out[3..7] read raw[6] low-nibble,
// raw[7..10] as full bytes, and raw[11] low-nibble. Validated against the SDK
// (raw 010f020102000703dcef… → 1f2120703dcef2b1).
func (e *EFW) SerialZWO() (string, error) {
	raw, _, err := e.Serial()
	if err != nil {
		return "", err
	}
	if len(raw) < 12 {
		return "", errors.New("short serial reply")
	}
	n := [16]byte{
		raw[0] & 0xf, raw[1] & 0xf, raw[2] & 0xf, raw[3] & 0xf,
		raw[4] & 0xf, raw[5] & 0xf, raw[6] & 0xf, raw[7] >> 4,
		raw[7] & 0xf, raw[8] >> 4, raw[8] & 0xf, raw[9] >> 4,
		raw[9] & 0xf, raw[10] >> 4, raw[10] & 0xf, raw[11] & 0xf,
	}
	out := make([]byte, 8)
	for i := range out {
		out[i] = n[2*i]<<4 | n[2*i+1]
	}
	return hex.EncodeToString(out), nil
}

// Alias reads the user-settable alias via the [03 7E 5A 02 0D] query and returns
// the raw reply payload (reply[4:]) plus its hex string. Read-only. Unset aliases
// read back as zeros — unlike the factory Serial, which is always populated.
func (e *EFW) Alias() (raw []byte, hexStr string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	q := make([]byte, e.featureLen)
	q[0], q[1], q[2], q[3], q[4] = repIDCmd, sig0, sig1, opQuery, subAlias
	if err := e.t.SetFeature(q); err != nil {
		return nil, "", fmt.Errorf("alias query: %w", err)
	}
	r := make([]byte, e.featureLen)
	r[0] = repIDReply
	if err := e.t.GetFeature(r); err != nil {
		return nil, "", fmt.Errorf("alias read: %w", err)
	}
	end := 16
	if len(r) < end {
		end = len(r)
	}
	raw = append([]byte(nil), r[4:end]...)
	return raw, hex.EncodeToString(raw), nil
}

// SetAlias writes the user alias ([03 7E 5A 0D <≤8 bytes>]) — a PERSISTENT flash
// write that changes device-stored state. At most 8 bytes are written (longer
// input is truncated; shorter is zero-padded). Reversible by writing zeros. The
// alias is read back via Alias(); it is independent of the factory Serial.
func (e *EFW) SetAlias(alias []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := make([]byte, e.featureLen)
	c[0], c[1], c[2], c[3] = repIDCmd, sig0, sig1, opWriteAlias
	copy(c[4:4+aliasLen], alias) // ≤8 bytes; remainder stays zero
	if err := e.t.SetFeature(c); err != nil {
		return fmt.Errorf("set alias: %w", err)
	}
	time.Sleep(moveSettle) // flash write settle
	return nil
}

// ClearAlias resets the user alias to all zeros (the unset state) — a persistent
// flash write that reverses SetAlias.
func (e *EFW) ClearAlias() error { return e.SetAlias(nil) }

// Handshake sends the open info query ([03 7E 5A 02 04]) and returns the reply,
// which carries firmware version + model. Read-only.
func (e *EFW) Handshake() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	q := make([]byte, e.featureLen)
	q[0], q[1], q[2], q[3], q[4] = repIDCmd, sig0, sig1, opQuery, subInfo
	if err := e.t.SetFeature(q); err != nil {
		return nil, fmt.Errorf("handshake query: %w", err)
	}
	r := make([]byte, e.featureLen)
	r[0] = repIDReply
	if err := e.t.GetFeature(r); err != nil {
		return nil, fmt.Errorf("handshake read: %w", err)
	}
	return r, nil
}

// FirmwareVersion returns (major, minor) from the handshake reply. The SDK reads
// the version from reply[4] (major) and reply[6] (minor) of the info query.
func (e *EFW) FirmwareVersion() (major, minor int, err error) {
	r, err := e.Handshake()
	if err != nil {
		return 0, 0, err
	}
	if len(r) <= 6 {
		return 0, 0, errors.New("short handshake reply")
	}
	return int(r[4]), int(r[6]), nil
}

// Model returns the device model string from the handshake reply (byte 8+, a
// null-terminated ASCII string, e.g. "EFW-S-0").
func (e *EFW) Model() (string, error) {
	r, err := e.Handshake()
	if err != nil {
		return "", err
	}
	if len(r) <= 8 {
		return "", nil
	}
	s := r[8:]
	for i, c := range s {
		if c == 0 {
			return string(s[:i]), nil
		}
	}
	return string(s), nil
}

// HWErrorCode returns the latched hardware error code (0 = none). The wheel
// reports an error as status state 0x06 with the code in byte 5.
func (e *EFW) HWErrorCode() (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.statusLocked()
	if err != nil {
		return 0, err
	}
	if len(r) > 5 && r[statusByteState] == stateError {
		return int(r[5]), nil
	}
	return 0, nil
}

// ClearError clears a latched error state ([03 7E 5A 01 0F], fire-and-forget).
func (e *EFW) ClearError() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := make([]byte, e.featureLen)
	c[0], c[1], c[2], c[3], c[4] = repIDCmd, sig0, sig1, opMotion, motClearErr
	if err := e.t.SetFeature(c); err != nil {
		return fmt.Errorf("clear error: %w", err)
	}
	return nil
}

// Calibrate runs the wheel's home + slot-realignment routine ([03 7E 5A 01 01],
// fire-and-forget). The wheel seeks its index/home reference and re-derives the
// exact step count to each slot so filters seat accurately centered. It spins the
// wheel for several seconds; poll Position (-1 while moving) for completion.
func (e *EFW) Calibrate() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := make([]byte, e.featureLen)
	c[0], c[1], c[2], c[3], c[4] = repIDCmd, sig0, sig1, opMotion, motCalibrate
	if err := e.t.SetFeature(c); err != nil {
		return fmt.Errorf("calibrate: %w", err)
	}
	time.Sleep(moveSettle)
	return nil
}

// Position returns the current 0-based slot, or -1 while the wheel is moving or
// its state is not idle. The wire reports a 1-based slot; we present 0-based to
// match the ASCOM convention (as the SDK does).
func (e *EFW) Position() (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.statusLocked()
	if err != nil {
		return -1, err
	}
	if len(r) <= statusBytePos {
		return -1, errors.New("short status report")
	}
	if r[statusByteState] != stateIdle { // moving/unsettled
		return -1, nil
	}
	return int(r[statusBytePos]) - 1, nil
}

// Slots returns the wheel's slot count (from the status report). Returns 0 if a
// status read fails.
func (e *EFW) Slots() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.statusLocked()
	if err != nil || len(r) <= statusByteSlots {
		return 0
	}
	return int(r[statusByteSlots])
}

// SetPosition moves the wheel to the given 0-based slot. Returns once the move
// is initiated (plus the MCU settle); poll Position for completion (-1 while
// moving) — the carousel physically rotates over seconds.
func (e *EFW) SetPosition(slot int) error {
	if slot < 0 || slot > 0x7f {
		return fmt.Errorf("slot %d out of range", slot)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	m := make([]byte, e.featureLen)
	m[0], m[1], m[2], m[3] = repIDCmd, sig0, sig1, opMotion
	m[4] = dirBidirectional
	if e.unidirectional {
		m[4] = dirUnidirectional
	}
	m[5] = byte(slot + 1) // device slots are 1-based on the wire
	if err := e.t.SetFeature(m); err != nil {
		return fmt.Errorf("move: %w", err)
	}
	time.Sleep(moveSettle)
	return nil
}
