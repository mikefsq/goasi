//go:build linux

package eaf

// Linux HID transport: pure Go over hidraw (/dev/hidrawN) using feature-report
// ioctls — no cgo, no external dependency. This is the deploy-target backend
// (incl. Raspberry Pi). Requires a udev rule so the service user can open the
// device, e.g.:
//
//	KERNEL=="hidraw*", ATTRS{idVendor}=="03c3", MODE="0660", TAG+="uaccess"

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const hidrawSysfs = "/sys/class/hidraw"

// _IOC encodes an ioctl request (asm-generic). hidraw feature ioctls use
// dir = _IOC_READ|_IOC_WRITE (3), type 'H', size = buffer length.
func iocRW(nr, size uintptr) uintptr {
	const dir = 3 // _IOC_READ | _IOC_WRITE
	return (dir << 30) | (size << 16) | (uintptr('H') << 8) | nr
}
func hidiocSFeature(n int) uintptr { return iocRW(0x06, uintptr(n)) } // HIDIOCSFEATURE(len)
func hidiocGFeature(n int) uintptr { return iocRW(0x07, uintptr(n)) } // HIDIOCGFEATURE(len)

type linuxDev struct {
	path     string // /dev/hidrawN
	vid, pid uint16
	loc      uint32 // hidraw number (session-stable; serial is the durable identity)
}

// scanHidraw lists hidraw devices and their VID/PID from sysfs.
func scanHidraw() []linuxDev {
	entries, err := os.ReadDir(hidrawSysfs)
	if err != nil {
		return nil
	}
	var out []linuxDev
	for _, e := range entries {
		name := e.Name() // "hidrawN"
		vid, pid, ok := parseUevent(filepath.Join(hidrawSysfs, name, "device", "uevent"))
		if !ok {
			continue
		}
		n, _ := strconv.Atoi(strings.TrimPrefix(name, "hidraw"))
		out = append(out, linuxDev{path: "/dev/" + name, vid: vid, pid: pid, loc: uint32(n)})
	}
	return out
}

// parseUevent reads "HID_ID=0003:000003C3:00001F01" (bus:vid:pid) from a hidraw
// device's uevent.
func parseUevent(path string) (vid, pid uint16, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "HID_ID=") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(line, "HID_ID="), ":")
		if len(parts) != 3 {
			return 0, 0, false
		}
		v, e1 := strconv.ParseUint(parts[1], 16, 32)
		p, e2 := strconv.ParseUint(parts[2], 16, 32)
		if e1 != nil || e2 != nil {
			return 0, 0, false
		}
		return uint16(v), uint16(p), true
	}
	return 0, 0, false
}

func matchEAF(d linuxDev) bool {
	return d.vid == VID && (d.pid == PIDEAF1 || d.pid == PIDEAF2)
}

type linuxTransport struct{ f *os.File }

func openLinux(d linuxDev) (Transport, DeviceInfo, error) {
	f, err := os.OpenFile(d.path, os.O_RDWR, 0)
	if err != nil {
		return nil, DeviceInfo{}, fmt.Errorf("open %s: %w (need a udev rule? "+
			`KERNEL=="hidraw*", ATTRS{idVendor}=="03c3", MODE="0660", TAG+="uaccess")`, d.path, err)
	}
	// hidraw does not expose the report length cheaply; the EAF uses 64-byte
	// reports (newEAF normalizes anyway).
	info := DeviceInfo{PID: d.pid, LocationID: d.loc, FeatureLen: 64}
	return &linuxTransport{f: f}, info, nil
}

// Enumerate lists all attached ZWO EAF hidraw devices.
func Enumerate() ([]DeviceInfo, error) {
	var out []DeviceInfo
	for _, d := range scanHidraw() {
		if matchEAF(d) {
			out = append(out, DeviceInfo{PID: d.pid, LocationID: d.loc})
		}
	}
	return out, nil
}

func openFirst() (Transport, DeviceInfo, error) {
	for _, d := range scanHidraw() {
		if matchEAF(d) {
			return openLinux(d)
		}
	}
	return nil, DeviceInfo{}, errors.New("no ZWO EAF found in /sys/class/hidraw")
}

// OpenLocation opens the EAF at a specific hidraw location (from Enumerate).
func OpenLocation(loc uint32) (Transport, DeviceInfo, error) {
	for _, d := range scanHidraw() {
		if matchEAF(d) && d.loc == loc {
			return openLinux(d)
		}
	}
	return nil, DeviceInfo{}, fmt.Errorf("no EAF at hidraw location %d", loc)
}

func (t *linuxTransport) ioctl(req uintptr, buf []byte) error {
	if len(buf) == 0 {
		return errors.New("empty buffer")
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, t.f.Fd(), req, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return errno
	}
	return nil
}

func (t *linuxTransport) SetFeature(buf []byte) error {
	if err := t.ioctl(hidiocSFeature(len(buf)), buf); err != nil {
		return fmt.Errorf("HIDIOCSFEATURE: %w", err)
	}
	return nil
}

func (t *linuxTransport) GetFeature(buf []byte) error {
	if err := t.ioctl(hidiocGFeature(len(buf)), buf); err != nil {
		return fmt.Errorf("HIDIOCGFEATURE: %w", err)
	}
	return nil
}

func (t *linuxTransport) Close() error { return t.f.Close() }
