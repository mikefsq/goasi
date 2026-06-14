//go:build windows

package eaf

// Windows HID transport: pure Go over hid.dll + SetupAPI via syscall.LazyDLL —
// no cgo, no external dependency. Vendor HID devices are user-accessible on
// Windows (no driver install needed, unlike the cameras).

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// GUID_DEVINTERFACE_HID
var hidGUID = guid{0x4D1E55B2, 0xF16F, 0x11CF, [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}

const (
	digcfPresent         = 0x02
	digcfDeviceInterface = 0x10
)

var (
	modSetupapi = syscall.NewLazyDLL("setupapi.dll")
	modHid      = syscall.NewLazyDLL("hid.dll")

	procGetClassDevs       = modSetupapi.NewProc("SetupDiGetClassDevsW")
	procEnumInterfaces     = modSetupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procGetInterfaceDetail = modSetupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procDestroyDeviceList  = modSetupapi.NewProc("SetupDiDestroyDeviceInfoList")

	procGetAttributes     = modHid.NewProc("HidD_GetAttributes")
	procSetFeature        = modHid.NewProc("HidD_SetFeature")
	procGetFeature        = modHid.NewProc("HidD_GetFeature")
	procGetPreparsedData  = modHid.NewProc("HidD_GetPreparsedData")
	procFreePreparsedData = modHid.NewProc("HidD_FreePreparsedData")
	procGetCaps           = modHid.NewProc("HidP_GetCaps")
)

type spDeviceInterfaceData struct {
	cbSize             uint32
	interfaceClassGuid guid
	flags              uint32
	reserved           uintptr
}

type hiddAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
}

type winDev struct {
	path     string
	vid, pid uint16
	loc      uint32 // FNV hash of the device path (stable per device)
}

func openHandle(path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return syscall.CreateFile(p,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil, syscall.OPEN_EXISTING, 0, 0)
}

// detailPath runs the two-call SetupDiGetDeviceInterfaceDetail pattern and
// returns the device interface path.
func detailPath(h uintptr, ida *spDeviceInterfaceData) string {
	var required uint32
	procGetInterfaceDetail.Call(h, uintptr(unsafe.Pointer(ida)), 0, 0, uintptr(unsafe.Pointer(&required)), 0)
	if required < 6 {
		return ""
	}
	buf := make([]byte, required)
	// cbSize of SP_DEVICE_INTERFACE_DETAIL_DATA_W: 8 on 64-bit, 6 on 32-bit.
	cb := uint32(8)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		cb = 6
	}
	*(*uint32)(unsafe.Pointer(&buf[0])) = cb
	r, _, _ := procGetInterfaceDetail.Call(h, uintptr(unsafe.Pointer(ida)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(required), 0, 0)
	if r == 0 {
		return ""
	}
	// DevicePath is a UTF-16 string at offset 4 (right after the DWORD cbSize).
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[4])), (required-4)/2)
	return syscall.UTF16ToString(u16)
}

func vidPID(path string) (vid, pid uint16, ok bool) {
	h, err := openHandle(path)
	if err != nil {
		return 0, 0, false
	}
	defer syscall.CloseHandle(h)
	var a hiddAttributes
	a.Size = uint32(unsafe.Sizeof(a))
	if r, _, _ := procGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&a))); r == 0 {
		return 0, 0, false
	}
	return a.VendorID, a.ProductID, true
}

func enumerateWindows() ([]winDev, error) {
	h, _, _ := procGetClassDevs.Call(uintptr(unsafe.Pointer(&hidGUID)), 0, 0, digcfPresent|digcfDeviceInterface)
	if h == ^uintptr(0) { // INVALID_HANDLE_VALUE
		return nil, errors.New("SetupDiGetClassDevs failed")
	}
	defer procDestroyDeviceList.Call(h)

	var out []winDev
	var ida spDeviceInterfaceData
	ida.cbSize = uint32(unsafe.Sizeof(ida))
	for i := 0; ; i++ {
		r, _, _ := procEnumInterfaces.Call(h, 0, uintptr(unsafe.Pointer(&hidGUID)),
			uintptr(i), uintptr(unsafe.Pointer(&ida)))
		if r == 0 {
			break // ERROR_NO_MORE_ITEMS
		}
		path := detailPath(h, &ida)
		if path == "" {
			continue
		}
		vid, pid, ok := vidPID(path)
		if !ok || vid != VID || (pid != PIDEAF1 && pid != PIDEAF2) {
			continue
		}
		out = append(out, winDev{path: path, vid: vid, pid: pid, loc: hashPath(path)})
	}
	return out, nil
}

func hashPath(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// featureLen reads the device's feature-report byte length (default 64).
func featureLen(h syscall.Handle) int {
	var pp uintptr
	if r, _, _ := procGetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&pp))); r == 0 {
		return 64
	}
	defer procFreePreparsedData.Call(pp)
	var caps [64]uint16 // HIDP_CAPS; FeatureReportByteLength is the 5th USHORT
	procGetCaps.Call(pp, uintptr(unsafe.Pointer(&caps[0])))
	if caps[4] == 0 {
		return 64
	}
	return int(caps[4])
}

type windowsTransport struct{ h syscall.Handle }

func openWindows(d winDev) (Transport, DeviceInfo, error) {
	h, err := openHandle(d.path)
	if err != nil {
		return nil, DeviceInfo{}, fmt.Errorf("open %s: %w", d.path, err)
	}
	info := DeviceInfo{PID: d.pid, LocationID: d.loc, FeatureLen: featureLen(h)}
	return &windowsTransport{h: h}, info, nil
}

// Enumerate lists all attached ZWO EAF HID devices.
func Enumerate() ([]DeviceInfo, error) {
	devs, err := enumerateWindows()
	if err != nil {
		return nil, err
	}
	out := make([]DeviceInfo, 0, len(devs))
	for _, d := range devs {
		out = append(out, DeviceInfo{PID: d.pid, LocationID: d.loc})
	}
	return out, nil
}

func openFirst() (Transport, DeviceInfo, error) {
	devs, err := enumerateWindows()
	if err != nil {
		return nil, DeviceInfo{}, err
	}
	if len(devs) == 0 {
		return nil, DeviceInfo{}, errors.New("no ZWO EAF found")
	}
	return openWindows(devs[0])
}

// OpenLocation opens the EAF whose path hashes to loc (from Enumerate).
func OpenLocation(loc uint32) (Transport, DeviceInfo, error) {
	devs, err := enumerateWindows()
	if err != nil {
		return nil, DeviceInfo{}, err
	}
	for _, d := range devs {
		if d.loc == loc {
			return openWindows(d)
		}
	}
	return nil, DeviceInfo{}, fmt.Errorf("no EAF at location 0x%08x", loc)
}

func (t *windowsTransport) SetFeature(buf []byte) error {
	if len(buf) == 0 {
		return errors.New("empty buffer")
	}
	r, _, err := procSetFeature.Call(uintptr(t.h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return fmt.Errorf("HidD_SetFeature: %v", err)
	}
	return nil
}

func (t *windowsTransport) GetFeature(buf []byte) error {
	if len(buf) == 0 {
		return errors.New("empty buffer")
	}
	r, _, err := procGetFeature.Call(uintptr(t.h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return fmt.Errorf("HidD_GetFeature: %v", err)
	}
	return nil
}

func (t *windowsTransport) Close() error { return syscall.CloseHandle(t.h) }
