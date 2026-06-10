package eaf

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// --- ZWO EAF HID framing. Same shape as the EFW: report ID 0x03 command /
// 0x01 reply, 7E 5A signature.
//
//	command : [0]=0x03 reportID  [1]=0x7E [2]=0x5A sig  [3]=opcode  [4..]=args
//	reply   : [0]=0x01 reportID  [1]=0x7E [2]=0x5A      [3]=opcode echo  [4..]=fields
//
// Unlike the EFW (which had a distinct opcode per command), the EAF has ONE write
// opcode — the control report (0x03) — that carries the full writable state every
// time. Each setter updates one cached field and re-emits the whole control report.
const (
	repIDCmd   = 0x03
	repIDReply = 0x01
	sig0       = 0x7E
	sig1       = 0x5A

	opControl = 0x03 // control/write: move/stop + reverse/beep/backlash/maxstep/speed
	opQuery   = 0x02 // query family ([4]=sub) and the open-info command
	opClear   = 0x01 // clear-error family ([4]=sub)

	subStatus = 0x03 // opQuery sub -> status report (step/temp/flags)
	subClear  = 0x0F // opClear sub -> clear latched error/limit

	stateError = 0x06 // status [4]: error/limit -> driver auto-issues clear
)

// moveSettle is the post-command settle after a move (the MCU accepting the
// command, not the physical travel — progress is observed via Position).
var moveSettle = 200 * time.Millisecond

// EAF is an opened auto-focuser.
type EAF struct {
	t          Transport
	info       DeviceInfo
	featureLen int

	mu sync.Mutex // serializes a command + its reply (per-device)

	// Cached control state, needed to rebuild the full control report. Populated by
	// the open handshake + each status read, updated by the setters.
	fw      int  // firmware version (maj*256+min), from the open-info reply
	use24   bool // fw >= 0x150: status carries a 24-bit current step
	maxStep int  // device max travel (status / open default)
	speed   int  // cached speed/maxforce
	state   byte // last status state byte ([5])
	beep    bool
	reverse bool
	curStep int // last reported current step
	moving  bool
}

func newEAF(t Transport, info DeviceInfo) *EAF {
	fl := info.FeatureLen
	if fl != 64 && fl != 16 {
		fl = 64 // descriptor reported something odd; the protocol uses 64 or 16
	}
	info.FeatureLen = fl
	return &EAF{t: t, info: info, featureLen: fl, maxStep: 60000}
}

// New wraps an already-open Transport as an EAF handle (custom backend or a fake
// for hardware-free testing). OpenFirst/OpenAt select a platform transport.
func New(t Transport, info DeviceInfo) *EAF { return newEAF(t, info) }

// OpenFirst finds and opens the first attached ZWO EAF, running the open handshake.
func OpenFirst() (*EAF, error) {
	t, info, err := openFirst()
	if err != nil {
		return nil, err
	}
	e := newEAF(t, info)
	e.handshake()
	return e, nil
}

// OpenAt opens the EAF at a specific USB locationID (from Enumerate / List).
func OpenAt(locationID uint32) (*EAF, error) {
	t, info, err := OpenLocation(locationID)
	if err != nil {
		return nil, err
	}
	e := newEAF(t, info)
	e.handshake()
	return e, nil
}

func (e *EAF) Info() DeviceInfo { return e.info }
func (e *EAF) FeatureLen() int  { return e.featureLen }
func (e *EAF) Close() error     { return e.t.Close() }

// command sends a feature report and, if want, reads the reply. Caller holds mu.
func (e *EAF) command(cmd []byte, want bool) ([]byte, error) {
	q := make([]byte, e.featureLen)
	q[0] = repIDCmd
	copy(q[1:], cmd) // cmd starts at [1]: sig0, sig1, opcode, args...
	if err := e.t.SetFeature(q); err != nil {
		return nil, fmt.Errorf("eaf: set feature: %w", err)
	}
	if !want {
		return nil, nil
	}
	r := make([]byte, e.featureLen)
	r[0] = repIDReply
	if err := e.t.GetFeature(r); err != nil {
		return nil, fmt.Errorf("eaf: get feature: %w", err)
	}
	return r, nil
}

// handshake issues the open-info command [7E 5A 02] and caches firmware/maxStep,
// then reads status once. Best-effort open-time init. Caller must not hold mu.
func (e *EAF) handshake() {
	e.mu.Lock()
	r, err := e.command([]byte{sig0, sig1, opQuery}, true)
	if err == nil && len(r) > 6 && r[1] == sig0 && r[2] == sig1 {
		// firmware version bytes (maj,min) live just past the echoed header.
		e.fw = int(r[5])<<8 | int(r[6])
		e.use24 = e.fw >= 0x150
	}
	e.mu.Unlock()
	_, _ = e.Status() // populate cached maxStep/speed/flags
}

// status issues the status query and parses it into the cache. Caller holds mu.
func (e *EAF) status() ([]byte, error) {
	r, err := e.command([]byte{sig0, sig1, opQuery, subStatus}, true)
	if err != nil {
		return nil, err
	}
	if len(r) < 16 || r[1] != sig0 || r[2] != sig1 || r[3] != opControl {
		return nil, errors.New("eaf: malformed status reply")
	}
	if r[4] == stateError { // error/limit -> auto-clear (best-effort)
		_, _ = e.command([]byte{sig0, sig1, opClear, subClear}, false)
	}
	e.state = r[5]
	if e.use24 {
		e.curStep = int(r[7])<<16 | int(r[8])<<8 | int(r[9])
		e.maxStep = int(r[6])<<16 | int(r[14])<<8 | int(r[15])
	} else {
		e.curStep = int(r[8])<<8 | int(r[9])
		e.maxStep = int(r[14])<<8 | int(r[15])
	}
	e.beep = r[13]&0x01 != 0
	e.reverse = r[13]&0x02 != 0
	e.moving = r[4] > 0 || r[13]&0x04 != 0
	return r, nil
}

// Status returns the raw status reply (after refreshing the cache), for debugging.
func (e *EAF) Status() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status()
}

// Position returns the current step (absolute). The EAF is geared (no clutch) with
// a real device-reported MaxStep, so absolute position is meaningful.
func (e *EAF) Position() (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.status(); err != nil {
		return 0, err
	}
	return e.curStep, nil
}

// IsMoving reports motion state from the device status report: the [4] move byte
// and the [d] status-flag bit (0x04).
func (e *EAF) IsMoving() (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.status(); err != nil {
		return false, err
	}
	return e.moving, nil
}

// MaxStep returns the device's maximum travel (from status).
func (e *EAF) MaxStep() (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.status(); err != nil {
		return 0, err
	}
	return e.maxStep, nil
}

// TemperatureRaw returns the raw 16-bit temperature field (big-endian) from status.
// The conversion to °C (a thermistor curve) is not yet implemented, so the raw
// value is exposed.
func (e *EAF) TemperatureRaw() (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.status()
	if err != nil {
		return 0, err
	}
	return int(r[11])<<8 | int(r[12]), nil
}

// control builds and sends the full control report (opcode 0x03) from the cached
// state plus the given move flag and target. Caller holds mu.
func (e *EAF) control(move byte, target int) error {
	c := []byte{
		sig0, sig1, opControl,
		move,                              // [4] 1=move, 0=stop
		e.state,                           // [5]
		byte(e.speed >> 8), byte(e.speed), // [6][7] speed/maxforce
		byte(target >> 8), byte(target), // [8][9] target hi/lo (16-bit)
		0,    // [a] sub-opcode (0 = normal control)
		0, 0, // [b][c]
		e.flags(),                             // [d] beep | reverse<<1
		byte(e.maxStep >> 8), byte(e.maxStep), // [e][f] maxStep hi/lo
	}
	_, err := e.command(c, false)
	return err
}

func (e *EAF) flags() byte {
	var f byte
	if e.beep {
		f |= 0x01
	}
	if e.reverse {
		f |= 0x02
	}
	return f
}

// MoveTo commands an absolute move to step (clamped to [0, MaxStep]). Returns once
// the command is acknowledged (plus the MCU settle); poll Position/IsMoving for
// completion. NOTE: the control report carries a 16-bit target; E-class focusers
// with MaxStep > 65535 need a 24-bit target encoding that is not yet decoded.
func (e *EAF) MoveTo(step int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if step < 0 {
		step = 0
	}
	if e.maxStep > 0 && step > e.maxStep {
		step = e.maxStep
	}
	if step > 0xffff {
		return errors.New("eaf: target > 65535 needs 24-bit control encoding (pending hardware)")
	}
	if err := e.control(1, step); err != nil {
		return err
	}
	time.Sleep(moveSettle)
	return nil
}

// Stop halts any in-progress move (control report with the move flag cleared).
func (e *EAF) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.control(0, e.curStep)
}

// SetReverse sets direction reversal (cached flag applied via the control report).
func (e *EAF) SetReverse(on bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reverse = on
	return e.control(0, e.curStep)
}

// SetBeep enables/disables the beeper (applied via the control report).
func (e *EAF) SetBeep(on bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.beep = on
	return e.control(0, e.curStep)
}

// ClearError clears a latched error/limit state ([7E 5A 01 0F], fire-and-forget).
func (e *EAF) ClearError() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err := e.command([]byte{sig0, sig1, opClear, subClear}, false)
	return err
}

// Listing is one entry from List: an enumerated EAF plus a queried snapshot.
type Listing struct {
	LocationID uint32
	PID        uint16
	Position   int
	MaxStep    int
}

// List enumerates every attached EAF and briefly opens each to read a snapshot.
func List() ([]Listing, error) {
	devs, err := Enumerate()
	if err != nil {
		return nil, err
	}
	out := make([]Listing, 0, len(devs))
	for _, d := range devs {
		l := Listing{LocationID: d.LocationID, PID: d.PID}
		if e, err := OpenAt(d.LocationID); err == nil {
			if p, err := e.Position(); err == nil {
				l.Position = p
			}
			if m, err := e.MaxStep(); err == nil {
				l.MaxStep = m
			}
			e.Close()
		}
		out = append(out, l)
	}
	return out, nil
}

// FirmwareVersion returns the cached firmware version (major, minor) from the open
// handshake.
func (e *EAF) FirmwareVersion() (major, minor int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.fw >> 8, e.fw & 0xff
}
