//go:build darwin

package efw

/*
#cgo darwin LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/hid/IOHIDDevice.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

// efw_enumerate lists all HID devices matching VID and either PID, filling
// outLoc[]/outPid[] (up to maxN entries). Returns the count, or -1 on failure.
static int efw_enumerate(uint32_t vid, uint32_t pid1, uint32_t pid2,
                         uint32_t* outLoc, uint32_t* outPid, int maxN) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return -1;
    IOHIDManagerSetDeviceMatching(mgr, NULL);
    IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);
    CFSetRef devs = IOHIDManagerCopyDevices(mgr);
    if (!devs) { CFRelease(mgr); return -1; }
    CFIndex n = CFSetGetCount(devs);
    int count = 0;
    if (n > 0) {
        const void** arr = (const void**)malloc(sizeof(void*) * n);
        CFSetGetValues(devs, arr);
        for (CFIndex i = 0; i < n && count < maxN; i++) {
            IOHIDDeviceRef d = (IOHIDDeviceRef)arr[i];
            int dvid = 0, dpid = 0; uint32_t dloc = 0;
            CFNumberRef v = (CFNumberRef)IOHIDDeviceGetProperty(d, CFSTR(kIOHIDVendorIDKey));
            CFNumberRef p = (CFNumberRef)IOHIDDeviceGetProperty(d, CFSTR(kIOHIDProductIDKey));
            CFNumberRef l = (CFNumberRef)IOHIDDeviceGetProperty(d, CFSTR(kIOHIDLocationIDKey));
            if (v) CFNumberGetValue(v, kCFNumberSInt32Type, &dvid);
            if (p) CFNumberGetValue(p, kCFNumberSInt32Type, &dpid);
            if (l) CFNumberGetValue(l, kCFNumberSInt32Type, &dloc);
            if ((uint32_t)dvid != vid) continue;
            if ((uint32_t)dpid != pid1 && (uint32_t)dpid != pid2) continue;
            outLoc[count] = dloc; outPid[count] = (uint32_t)dpid; count++;
        }
        free(arr);
    }
    CFRelease(devs); CFRelease(mgr);
    return count;
}

// efw_open finds a HID device with the given VID and either PID. If wantLoc != 0
// it must also match that USB locationID (else the first match is taken). Opens
// it and returns its IOHIDDeviceRef (void*) plus serial/product/maxFeature and
// the chosen device's locationID. Returns NULL if none found or open failed.
static void* efw_open(uint32_t vid, uint32_t pid1, uint32_t pid2, uint32_t wantLoc,
                      char* serial, int serialCap,
                      char* product, int productCap,
                      int* maxFeat, uint32_t* outLoc) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return NULL;

    // Match all, then filter by VID+PID(+location) in the loop (a VID-only
    // matching dict returned an empty set here; match-all + filter is robust).
    IOHIDManagerSetDeviceMatching(mgr, NULL);
    IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);

    CFSetRef devs = IOHIDManagerCopyDevices(mgr);
    if (!devs) { CFRelease(mgr); return NULL; }
    CFIndex n = CFSetGetCount(devs);
    if (n == 0) { CFRelease(devs); CFRelease(mgr); return NULL; }
    const void** arr = (const void**)malloc(sizeof(void*) * n);
    CFSetGetValues(devs, arr);

    IOHIDDeviceRef chosen = NULL;
    uint32_t chosenLoc = 0;
    for (CFIndex i = 0; i < n; i++) {
        IOHIDDeviceRef d = (IOHIDDeviceRef)arr[i];
        int dvid = 0, dpid = 0; uint32_t dloc = 0;
        CFNumberRef v = (CFNumberRef)IOHIDDeviceGetProperty(d, CFSTR(kIOHIDVendorIDKey));
        CFNumberRef p = (CFNumberRef)IOHIDDeviceGetProperty(d, CFSTR(kIOHIDProductIDKey));
        CFNumberRef l = (CFNumberRef)IOHIDDeviceGetProperty(d, CFSTR(kIOHIDLocationIDKey));
        if (v) CFNumberGetValue(v, kCFNumberSInt32Type, &dvid);
        if (p) CFNumberGetValue(p, kCFNumberSInt32Type, &dpid);
        if (l) CFNumberGetValue(l, kCFNumberSInt32Type, &dloc);
        if ((uint32_t)dvid != vid) continue;
        if ((uint32_t)dpid != pid1 && (uint32_t)dpid != pid2) continue;
        if (wantLoc != 0 && dloc != wantLoc) continue;
        chosen = d; chosenLoc = dloc; break;
    }
    if (!chosen) {
        free(arr); CFRelease(devs); CFRelease(mgr); return NULL;
    }
    CFRetain(chosen); // survive release of devs/mgr
    if (outLoc) *outLoc = chosenLoc;

    if (serial && serialCap > 0) {
        serial[0] = 0;
        CFStringRef s = (CFStringRef)IOHIDDeviceGetProperty(chosen, CFSTR(kIOHIDSerialNumberKey));
        if (s) CFStringGetCString(s, serial, serialCap, kCFStringEncodingUTF8);
    }
    if (product && productCap > 0) {
        product[0] = 0;
        CFStringRef s = (CFStringRef)IOHIDDeviceGetProperty(chosen, CFSTR(kIOHIDProductKey));
        if (s) CFStringGetCString(s, product, productCap, kCFStringEncodingUTF8);
    }
    if (maxFeat) {
        *maxFeat = 0;
        CFNumberRef m = (CFNumberRef)IOHIDDeviceGetProperty(chosen, CFSTR(kIOHIDMaxFeatureReportSizeKey));
        if (m) CFNumberGetValue(m, kCFNumberSInt32Type, maxFeat);
    }

    free(arr);
    CFRelease(devs);

    IOReturn r = IOHIDDeviceOpen(chosen, kIOHIDOptionsTypeNone);
    if (r != kIOReturnSuccess) {
        // open failed (0xe00002e2 = not permitted); the Go caller surfaces the error.
        CFRelease(chosen); CFRelease(mgr); return NULL;
    }
    // mgr is intentionally retained (not released) so its matching stays alive.
    return (void*)chosen;
}

static int efw_setfeature(void* dev, uint8_t* buf, int len) {
    if (len < 1) return -1;
    IOReturn r = IOHIDDeviceSetReport((IOHIDDeviceRef)dev, kIOHIDReportTypeFeature,
                                      (CFIndex)buf[0], buf, (CFIndex)len);
    return (r == kIOReturnSuccess) ? 0 : (int)r;
}

static int efw_getfeature(void* dev, uint8_t* buf, int len) {
    if (len < 1) return -1;
    CFIndex rl = (CFIndex)len;
    IOReturn r = IOHIDDeviceGetReport((IOHIDDeviceRef)dev, kIOHIDReportTypeFeature,
                                      (CFIndex)buf[0], buf, &rl);
    return (r == kIOReturnSuccess) ? (int)rl : -1;
}

static void efw_close(void* dev) {
    if (dev) {
        IOHIDDeviceClose((IOHIDDeviceRef)dev, kIOHIDOptionsTypeNone);
        CFRelease((IOHIDDeviceRef)dev);
    }
}

// efw_debug_list enumerates ALL HID devices (no matching filter) and prints
// vid/pid/product to stdout. Returns the device count, or -1 if the manager
// produced no device set (enumeration blocked / permission).
static int efw_debug_list(void) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return -2;
    IOHIDManagerSetDeviceMatching(mgr, NULL); // NULL = match everything
    IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);
    CFSetRef devs = IOHIDManagerCopyDevices(mgr);
    if (!devs) { CFRelease(mgr); return -1; }
    CFIndex n = CFSetGetCount(devs);
    if (n > 0) {
        const void** arr = (const void**)malloc(sizeof(void*) * n);
        CFSetGetValues(devs, arr);
        for (CFIndex i = 0; i < n; i++) {
            IOHIDDeviceRef d = (IOHIDDeviceRef)arr[i];
            int vid = 0, pid = 0;
            CFNumberRef v = (CFNumberRef)IOHIDDeviceGetProperty(d, CFSTR(kIOHIDVendorIDKey));
            CFNumberRef p = (CFNumberRef)IOHIDDeviceGetProperty(d, CFSTR(kIOHIDProductIDKey));
            if (v) CFNumberGetValue(v, kCFNumberSInt32Type, &vid);
            if (p) CFNumberGetValue(p, kCFNumberSInt32Type, &pid);
            char prod[256]; prod[0] = 0;
            CFStringRef s = (CFStringRef)IOHIDDeviceGetProperty(d, CFSTR(kIOHIDProductKey));
            if (s) CFStringGetCString(s, prod, sizeof(prod), kCFStringEncodingUTF8);
            printf("  vid=0x%04x pid=0x%04x  %s\n", vid, pid, prod);
        }
        free(arr);
    }
    CFRelease(devs);
    CFRelease(mgr);
    return (int)n;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// DebugListAll prints every HID device IOKit can see, for diagnosing why a wheel
// isn't found. Returns the count, or a negative value if enumeration was blocked
// (e.g. permission). Non-darwin builds provide a no-op returning -1.
func DebugListAll() int { return int(C.efw_debug_list()) }

type iokitTransport struct{ dev unsafe.Pointer }

// Enumerate lists all attached ZWO EFW HID devices (by USB locationID + PID).
// The ZWO factory serial is NOT in the USB layer, so callers that need it must
// open each device and query (see efw.List).
func Enumerate() ([]DeviceInfo, error) {
	const maxN = 32
	var locs [maxN]C.uint32_t
	var pids [maxN]C.uint32_t
	n := int(C.efw_enumerate(C.uint32_t(VID), C.uint32_t(PIDEFW1), C.uint32_t(PIDEFW2), &locs[0], &pids[0], maxN))
	if n < 0 {
		return nil, errors.New("HID enumeration failed")
	}
	out := make([]DeviceInfo, n)
	for i := 0; i < n; i++ {
		out[i] = DeviceInfo{PID: uint16(pids[i]), LocationID: uint32(locs[i])}
	}
	return out, nil
}

// openDev opens a device by VID/PID; wantLoc==0 takes the first match, else the
// device at that USB locationID.
func openDev(wantLoc uint32) (Transport, DeviceInfo, error) {
	var serial [256]C.char
	var product [256]C.char
	var maxFeat C.int
	var loc C.uint32_t
	dev := C.efw_open(
		C.uint32_t(VID), C.uint32_t(PIDEFW1), C.uint32_t(PIDEFW2), C.uint32_t(wantLoc),
		&serial[0], 256, &product[0], 256, &maxFeat, &loc,
	)
	if dev == nil {
		return nil, DeviceInfo{}, errors.New("no matching ZWO EFW (none attached, another process holds it, or HID access denied)")
	}
	info := DeviceInfo{
		Serial:     C.GoString(&serial[0]),
		Product:    C.GoString(&product[0]),
		FeatureLen: int(maxFeat),
		LocationID: uint32(loc),
	}
	return &iokitTransport{dev: dev}, info, nil
}

func openFirst() (Transport, DeviceInfo, error) { return openDev(0) }

// OpenLocation opens the EFW at a specific USB locationID (from Enumerate).
func OpenLocation(loc uint32) (Transport, DeviceInfo, error) { return openDev(loc) }

func (t *iokitTransport) SetFeature(buf []byte) error {
	if len(buf) == 0 {
		return errors.New("empty buffer")
	}
	if rc := C.efw_setfeature(t.dev, (*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf))); rc != 0 {
		return fmt.Errorf("IOHIDDeviceSetReport(feature) failed: rc=0x%x", int(rc))
	}
	return nil
}

func (t *iokitTransport) GetFeature(buf []byte) error {
	if len(buf) == 0 {
		return errors.New("empty buffer")
	}
	if rc := C.efw_getfeature(t.dev, (*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf))); rc < 0 {
		return errors.New("IOHIDDeviceGetReport(feature) failed")
	}
	return nil
}

func (t *iokitTransport) Close() error {
	if t.dev != nil {
		C.efw_close(t.dev)
		t.dev = nil
	}
	return nil
}
