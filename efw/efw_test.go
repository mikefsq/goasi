package efw

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

// fakeHID is an in-memory Transport: it records the bytes we send (to assert
// command encoding) and returns canned replies (to assert parsing) — so the whole
// EFW logic is testable with no hardware and no cgo.
type fakeHID struct {
	mu        sync.Mutex
	sent      [][]byte           // every SetFeature payload
	replies   map[[2]byte][]byte // (opcode, subcode) -> full reply bytes
	statusSeq [][]byte           // successive status replies (simulates motion)
	last      [2]byte            // last query's (opcode, subcode)
	failNext  bool               // simulate a removed device (transport error)
	closed    bool
}

func newFake() *fakeHID { return &fakeHID{replies: map[[2]byte][]byte{}} }

func (f *fakeHID) SetFeature(b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		return errors.New("device removed")
	}
	f.sent = append(f.sent, append([]byte(nil), b...))
	if len(b) >= 5 {
		f.last = [2]byte{b[3], b[4]}
	}
	return nil
}

func (f *fakeHID) GetFeature(b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		return errors.New("device removed")
	}
	if f.last == ([2]byte{opQuery, subStatus}) && len(f.statusSeq) > 0 {
		copy(b, f.statusSeq[0])
		if len(f.statusSeq) > 1 { // keep the last one once the ramp is exhausted
			f.statusSeq = f.statusSeq[1:]
		}
		return nil
	}
	if r, ok := f.replies[f.last]; ok {
		copy(b, r)
	}
	return nil
}

func (f *fakeHID) Close() error { f.mu.Lock(); f.closed = true; f.mu.Unlock(); return nil }

func (f *fakeHID) firstSent() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return nil
	}
	return f.sent[0]
}

// sentWithPrefix returns the first recorded payload beginning with p, or nil.
// SetPosition may issue a status query before the move to learn the slot count,
// so a move command isn't always sent[0].
func (f *fakeHID) sentWithPrefix(p []byte) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.sent {
		if bytes.HasPrefix(s, p) {
			return s
		}
	}
	return nil
}

func testEFW(f *fakeHID) *EFW { return newEFW(f, DeviceInfo{FeatureLen: 64, Product: "ZWO EFW"}) }

// Fixtures — captured
var (
	fxStatusIdle   = []byte{0x01, 0x7e, 0x5a, 0x01, 0x01, 0x00, 0x01, 0x01, 0x01, 0x07, 0x00, 0x00, 0x00, 0x00, 0x2b, 0x01}
	fxStatusMoving = []byte{0x01, 0x7e, 0x5a, 0x01, 0x04, 0x00, 0x04, 0x02, 0x03, 0x07, 0x00, 0x00, 0x00, 0x00, 0x2b, 0x01}
	fxStatusAt3    = []byte{0x01, 0x7e, 0x5a, 0x01, 0x01, 0x00, 0x04, 0x04, 0x04, 0x07, 0x00, 0x00, 0x00, 0x00, 0x2b, 0x01}
	// Captured: a unidirectional move that faulted at the wrap — state 0x06,
	// error code 0x0c in byte 5, stuck at wire slot 7.
	fxStatusError = []byte{0x01, 0x7e, 0x5a, 0x01, 0x06, 0x0c, 0x01, 0x07, 0x01, 0x07, 0x00, 0x00, 0x00, 0x00, 0x30, 0x00}
	fxSerial      = []byte{0x01, 0x7e, 0x5a, 0x0c, 0x01, 0x0f, 0x02, 0x01, 0x02, 0x00, 0x07, 0x03, 0xdc, 0xef, 0x2b, 0x01}
	fxHandshake    = []byte{0x01, 0x7e, 0x5a, 0x04, 0x03, 0x00, 0x09, 0x00, 0x45, 0x46, 0x57, 0x2d, 0x53, 0x2d, 0x30, 0x00}
)

// --- Encode: the exact wire bytes for each command ---

func TestEncodeCommands(t *testing.T) {
	moveSettle = 0 // don't sleep in tests

	cases := []struct {
		name string
		do   func(*EFW)
		want []byte
	}{
		{"move bidi slot3", func(e *EFW) { e.SetPosition(3) }, []byte{0x03, 0x7e, 0x5a, 0x01, 0x02, 0x04}},
		{"move uni slot3", func(e *EFW) { e.SetUnidirectional(true); e.SetPosition(3) }, []byte{0x03, 0x7e, 0x5a, 0x01, 0x03, 0x04}},
		{"calibrate", func(e *EFW) { e.Calibrate() }, []byte{0x03, 0x7e, 0x5a, 0x01, 0x01}},
		{"clear error", func(e *EFW) { e.ClearError() }, []byte{0x03, 0x7e, 0x5a, 0x01, 0x0f}},
		{"set alias", func(e *EFW) { e.SetAlias([]byte("EFW7")) }, []byte{0x03, 0x7e, 0x5a, 0x0d, 'E', 'F', 'W', '7'}},
		{"status query", func(e *EFW) { e.Position() }, []byte{0x03, 0x7e, 0x5a, 0x02, 0x01}},
		{"serial query", func(e *EFW) { e.Serial() }, []byte{0x03, 0x7e, 0x5a, 0x02, 0x0c}},
		{"alias query", func(e *EFW) { e.Alias() }, []byte{0x03, 0x7e, 0x5a, 0x02, 0x0d}},
		{"handshake", func(e *EFW) { e.Handshake() }, []byte{0x03, 0x7e, 0x5a, 0x02, 0x04}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake()
			c.do(testEFW(f))
			got := f.sentWithPrefix(c.want)
			if got == nil {
				t.Errorf("no command with prefix % x (sent %d)", c.want, len(f.sent))
			}
		})
	}
}

// SetPosition must reject a slot the wheel doesn't have. A 7-slot wheel is
// 0..6; asking for slot 7 (or higher) previously sent wire-slot 8, which the
// MCU silently ignores — the tool "accepted" the move but the motor never ran.
func TestSetPositionRejectsOutOfRange(t *testing.T) {
	moveSettle = 0
	f := newFake()
	f.replies[[2]byte{opQuery, subStatus}] = fxStatusIdle // 7-slot wheel
	e := testEFW(f)

	if err := e.SetPosition(7); err == nil {
		t.Error("SetPosition(7) on a 7-slot wheel: want error, got nil")
	}
	if got := f.sentWithPrefix([]byte{repIDCmd, sig0, sig1, opMotion}); got != nil {
		t.Errorf("rejected move must send no motion command; sent % x", got)
	}

	// The last valid slot (6) must still be accepted and produce a move command.
	if err := e.SetPosition(6); err != nil {
		t.Errorf("SetPosition(6) on a 7-slot wheel: want nil, got %v", err)
	}
	if got := f.sentWithPrefix([]byte{repIDCmd, sig0, sig1, opMotion}); got == nil {
		t.Error("SetPosition(6): no motion command sent")
	}
}

// A latched hardware fault must surface through Position (wrapping ErrWheelError
// with the code) rather than masquerade as -1/"still moving" — otherwise a poll
// loop waits forever on a move that will never settle.
func TestPositionSurfacesHardwareError(t *testing.T) {
	f := newFake()
	f.replies[[2]byte{opQuery, subStatus}] = fxStatusError
	e := testEFW(f)

	p, err := e.Position()
	if p != -1 {
		t.Errorf("Position on fault: got pos %d want -1", p)
	}
	if !errors.Is(err, ErrWheelError) {
		t.Fatalf("Position on fault: err=%v want ErrWheelError", err)
	}
	if code, _ := e.HWErrorCode(); code != 0x0c {
		t.Errorf("HWErrorCode=%d want 12", code)
	}
}

// A unidirectional move back by exactly one slot (forward distance n-1) faults
// this firmware, so — like ZWO's driver, confirmed by USB capture — the wheel is
// sent through an intermediate slot half a revolution forward: on a 7-slot wheel
// slot 1 -> 0 becomes 1 -> 4 (settle) -> 0 (3 + 3 steps).
func TestUnidirectionalReverseSplit(t *testing.T) {
	moveSettle = 0
	splitPoll = 0
	idleAt := func(wire byte) []byte {
		return []byte{0x01, 0x7e, 0x5a, 0x01, 0x01, 0x00, wire, wire, wire, 0x07, 0, 0, 0, 0, 0x2b, 0x01}
	}
	f := newFake()
	// SetPosition reads idle@wire2 (slot1); the intermediate hop then shows moving,
	// then idle@wire5 (slot4) so the wait completes and the final hop is issued.
	f.statusSeq = [][]byte{idleAt(0x02), fxStatusMoving, idleAt(0x05)}
	e := testEFW(f)
	e.SetUnidirectional(true)

	if err := e.SetPosition(0); err != nil { // slot 1 -> 0: forward distance 6 (worst case)
		t.Fatalf("SetPosition(0): %v", err)
	}

	// Expect a uni move to the intermediate slot 4 (wire5) then to slot 0 (wire1).
	mid := []byte{repIDCmd, sig0, sig1, opMotion, dirUnidirectional, 0x05}
	final := []byte{repIDCmd, sig0, sig1, opMotion, dirUnidirectional, 0x01}
	iMid, iFinal := -1, -1
	for i, s := range f.sent {
		if iMid < 0 && bytes.HasPrefix(s, mid) {
			iMid = i
		}
		if iFinal < 0 && bytes.HasPrefix(s, final) {
			iFinal = i
		}
	}
	switch {
	case iMid < 0:
		t.Error("no intermediate move to slot 4 (wire5) — split not performed")
	case iFinal < 0:
		t.Error("no final move to slot 0 (wire1)")
	case iMid > iFinal:
		t.Errorf("intermediate move (idx %d) must precede final move (idx %d)", iMid, iFinal)
	}
}

// A unidirectional move that is NOT the worst case must stay a single command —
// e.g. slot 5 -> 3 (forward distance 5) never splits (matches ZWO on the wire).
func TestUnidirectionalNoSplitWhenNotWorstCase(t *testing.T) {
	moveSettle = 0
	f := newFake()
	f.replies[[2]byte{opQuery, subStatus}] = []byte{0x01, 0x7e, 0x5a, 0x01, 0x01, 0x00, 0x06, 0x06, 0x06, 0x07, 0, 0, 0, 0, 0x2b, 0x01} // idle at wire6 (slot5)
	e := testEFW(f)
	e.SetUnidirectional(true)

	if err := e.SetPosition(3); err != nil { // slot 5 -> 3: forward distance 5
		t.Fatalf("SetPosition(3): %v", err)
	}
	moves := 0
	for _, s := range f.sent {
		if bytes.HasPrefix(s, []byte{repIDCmd, sig0, sig1, opMotion}) {
			moves++
		}
	}
	if moves != 1 {
		t.Errorf("slot 5->3 uni: got %d move commands, want 1 (no split)", moves)
	}
}

// --- Decode: real replies parse to the right values ---

func TestDecodePositionAndSlots(t *testing.T) {
	f := newFake()
	f.replies[[2]byte{opQuery, subStatus}] = fxStatusIdle
	e := testEFW(f)
	if pos, err := e.Position(); err != nil || pos != 0 {
		t.Fatalf("Position()=%d,%v want 0,nil", pos, err)
	}
	if s := e.Slots(); s != 7 {
		t.Errorf("Slots()=%d want 7", s)
	}
}

func TestDecodeSerialZWO(t *testing.T) {
	f := newFake()
	f.replies[[2]byte{opQuery, subSerial}] = fxSerial
	if s, err := testEFW(f).SerialZWO(); err != nil || s != "1f2120703dcef2b1" {
		t.Fatalf("SerialZWO()=%q,%v want 1f2120703dcef2b1", s, err)
	}
}

func TestDecodeFirmwareAndModel(t *testing.T) {
	f := newFake()
	f.replies[[2]byte{opQuery, subInfo}] = fxHandshake
	e := testEFW(f)
	if maj, min, err := e.FirmwareVersion(); err != nil || maj != 3 || min != 9 {
		t.Fatalf("FirmwareVersion()=%d.%d,%v want 3.9", maj, min, err)
	}
	if m, err := e.Model(); err != nil || m != "EFW-S-0" {
		t.Fatalf("Model()=%q,%v want EFW-S-0", m, err)
	}
}

func TestDecodeHWError(t *testing.T) {
	errStatus := append([]byte(nil), fxStatusIdle...)
	errStatus[statusByteState] = stateError
	errStatus[5] = 0x2a
	f := newFake()
	f.replies[[2]byte{opQuery, subStatus}] = errStatus
	if code, err := testEFW(f).HWErrorCode(); err != nil || code != 0x2a {
		t.Fatalf("HWErrorCode()=%d,%v want 42", code, err)
	}
}

// --- State machine: -1 while moving, then the arrived slot ---

func TestMoveSettlesToTarget(t *testing.T) {
	moveSettle = 0
	f := newFake()
	// The first frame is consumed by SetPosition's slot-count read (it learns the
	// wheel size before moving); the rest drive the poll loop below.
	f.statusSeq = [][]byte{fxStatusMoving, fxStatusMoving, fxStatusMoving, fxStatusAt3}
	e := testEFW(f)
	if err := e.SetPosition(3); err != nil {
		t.Fatal(err)
	}
	var got []int
	for i := 0; i < 4; i++ {
		p, _ := e.Position()
		got = append(got, p)
	}
	want := []int{-1, -1, 3, 3} // moving, moving, arrived, stays
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("poll %d: got %d want %d (seq %v)", i, got[i], want[i], got)
		}
	}
}

// --- Removal: a transport error surfaces, never panics ---

func TestDeviceRemoved(t *testing.T) {
	moveSettle = 0
	f := newFake()
	f.failNext = true
	e := testEFW(f)
	if _, err := e.Position(); err == nil {
		t.Error("Position: want error on removed device")
	}
	if err := e.SetPosition(2); err == nil {
		t.Error("SetPosition: want error on removed device")
	}
}

// --- Concurrency: the per-device lock holds under -race ---

func TestConcurrentAccess(t *testing.T) {
	moveSettle = 0
	f := newFake()
	f.replies[[2]byte{opQuery, subStatus}] = fxStatusIdle
	f.replies[[2]byte{opQuery, subSerial}] = fxSerial
	e := testEFW(f)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 4 {
			case 0:
				e.Position()
			case 1:
				e.SetPosition(i % 7)
			case 2:
				e.Serial()
			case 3:
				e.Slots()
			}
		}(i)
	}
	wg.Wait()
}
