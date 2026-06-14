package eaf

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

// fakeHID is an in-memory Transport: records command feature reports and returns a
// canned status reply, so the protocol layer is testable with no hardware/cgo.
type fakeHID struct {
	mu       sync.Mutex
	sent     [][]byte
	status   []byte // reply to a status query (opQuery/subStatus)
	failNext bool
}

func (f *fakeHID) SetFeature(b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		return errors.New("device removed")
	}
	f.sent = append(f.sent, append([]byte(nil), b...))
	return nil
}

func (f *fakeHID) GetFeature(b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		return errors.New("device removed")
	}
	if len(f.sent) > 0 {
		last := f.sent[len(f.sent)-1]
		if len(last) > 4 && last[3] == opQuery && last[4] == subStatus && f.status != nil {
			copy(b, f.status)
		}
	}
	return nil
}

func (f *fakeHID) Close() error { return nil }

func (f *fakeHID) last() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return nil
	}
	return f.sent[len(f.sent)-1]
}

func testEAF(f *fakeHID) *EAF { return newEAF(f, DeviceInfo{FeatureLen: 64, PID: PIDEAF1}) }

// statusReply (16-bit path): pos and maxStep at [8][9]/[e][f], state idle.
func statusReply(pos, max int, moving byte) []byte {
	r := make([]byte, 16)
	r[0], r[1], r[2], r[3] = repIDReply, sig0, sig1, opControl
	r[4] = moving
	r[8], r[9] = byte(pos>>8), byte(pos)
	r[11], r[12] = 0x01, 0x2C // temp raw
	r[14], r[15] = byte(max>>8), byte(max)
	return r
}

func TestEncodeCommands(t *testing.T) {
	moveSettle = 0
	cases := []struct {
		name string
		do   func(*EAF)
		want []byte
	}{
		{"status query", func(e *EAF) { e.Status() }, []byte{repIDCmd, sig0, sig1, opQuery, subStatus}},
		{"move to 3000", func(e *EAF) { e.MoveTo(3000) }, []byte{repIDCmd, sig0, sig1, opControl, 0x01, 0x00, 0x00, 0x00, 0x0B, 0xB8}},
		{"stop", func(e *EAF) { e.Stop() }, []byte{repIDCmd, sig0, sig1, opControl, 0x00}},
		{"clear error", func(e *EAF) { e.ClearError() }, []byte{repIDCmd, sig0, sig1, opClear, subClear}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeHID{status: statusReply(0, 60000, 0)}
			c.do(testEAF(f))
			got := f.last()
			if got == nil || !bytes.HasPrefix(got, c.want) {
				t.Errorf("got % x, want prefix % x", got, c.want)
			}
		})
	}
}

func TestDecodeStatus(t *testing.T) {
	f := &fakeHID{status: statusReply(5000, 60000, 0)}
	e := testEAF(f)
	if p, err := e.Position(); err != nil || p != 5000 {
		t.Fatalf("Position()=%d,%v want 5000", p, err)
	}
	if m, err := e.MaxStep(); err != nil || m != 60000 {
		t.Fatalf("MaxStep()=%d,%v want 60000", m, err)
	}
	if mv, err := e.IsMoving(); err != nil || mv {
		t.Fatalf("IsMoving()=%v,%v want false", mv, err)
	}
	f.status = statusReply(5000, 60000, 1)
	if mv, err := e.IsMoving(); err != nil || !mv {
		t.Fatalf("IsMoving()=%v,%v want true", mv, err)
	}
}

func TestMoveClampsToMaxStep(t *testing.T) {
	moveSettle = 0
	f := &fakeHID{status: statusReply(0, 60000, 0)}
	e := testEAF(f)
	e.MaxStep() // refresh cache -> maxStep 60000
	if err := e.MoveTo(100000); err != nil {
		t.Fatal(err)
	}
	got := f.last() // target clamped to maxStep 60000 = 0xEA60 at [8][9]
	if got[8] != 0xEA || got[9] != 0x60 {
		t.Fatalf("clamped target = %02x%02x, want ea60", got[8], got[9])
	}
}

func TestDeviceRemoved(t *testing.T) {
	f := &fakeHID{failNext: true}
	if _, err := testEAF(f).Position(); err == nil {
		t.Error("want error on removed device")
	}
}

func TestConcurrentAccess(t *testing.T) {
	moveSettle = 0
	f := &fakeHID{status: statusReply(1000, 60000, 0)}
	e := testEAF(f)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				e.Position()
			case 1:
				e.MoveTo(i % 5000)
			case 2:
				e.IsMoving()
			}
		}(i)
	}
	wg.Wait()
}
