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

func testEFW(f *fakeHID) *EFW { return newEFW(f, DeviceInfo{FeatureLen: 64, Product: "ZWO EFW"}) }

// Fixtures — captured
var (
	fxStatusIdle   = []byte{0x01, 0x7e, 0x5a, 0x01, 0x01, 0x00, 0x01, 0x01, 0x01, 0x07, 0x00, 0x00, 0x00, 0x00, 0x2b, 0x01}
	fxStatusMoving = []byte{0x01, 0x7e, 0x5a, 0x01, 0x04, 0x00, 0x04, 0x02, 0x03, 0x07, 0x00, 0x00, 0x00, 0x00, 0x2b, 0x01}
	fxStatusAt3    = []byte{0x01, 0x7e, 0x5a, 0x01, 0x01, 0x00, 0x04, 0x04, 0x04, 0x07, 0x00, 0x00, 0x00, 0x00, 0x2b, 0x01}
	fxSerial       = []byte{0x01, 0x7e, 0x5a, 0x0c, 0x01, 0x0f, 0x02, 0x01, 0x02, 0x00, 0x07, 0x03, 0xdc, 0xef, 0x2b, 0x01}
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
			got := f.firstSent()
			if got == nil {
				t.Fatal("no command sent")
			}
			if !bytes.HasPrefix(got, c.want) {
				t.Errorf("got % x, want prefix % x", got, c.want)
			}
		})
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
	f.statusSeq = [][]byte{fxStatusMoving, fxStatusMoving, fxStatusAt3}
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
