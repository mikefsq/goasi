# efw — pure-Go ZWO EFW (filter wheel) driver

The `efw` package of the `goasi` module (`github.com/mikefsq/goasi/efw`): a
**cgo-free** driver for the ZWO EFW (Electronic Filter Wheel). It talks the
device's USB-HID feature-report protocol directly — no vendor SDK runtime
dependency in the process — and is validated against real hardware.

## Layout

Paths are relative to the `goasi` module root (the parent of `efw/`); run the
`go` commands below from there.

```
efw/
  efw.go               device logic + HID framing (pure Go, all platforms)
  transport.go         the Transport interface (the seam) + DeviceInfo
  transport_darwin.go  macOS   — IOKit (cgo)
  transport_linux.go   Linux   — hidraw ioctls (pure Go)
  transport_windows.go Windows — hid.dll + SetupAPI (pure Go)
  transport_stub.go    other   — compile-only
  efw_test.go          unit tests over a fake Transport (no hardware)
cmd/efwprobe/          CLI to enumerate / inspect / drive a wheel
```

Only the thin `transport_<os>.go` differs per platform; `efw.go` and the tests are
platform-independent.

## Status

| | |
|---|---|
| Coverage | Full device surface: move, calibrate, status/position, serial, alias read/write, firmware, model, HW error, clear-error. (Firmware *flashing* is intentionally omitted.) |
| macOS | hardware-validated |
| Linux / Windows | build + vet clean; runtime validation pending on hardware |
| Tests | `go test -race` over a fake Transport, with fixtures captured from a real wheel |

## Build `efwprobe`

macOS uses IOKit (cgo); Linux and Windows are pure Go and cross-compile from any
host to a static binary.

```sh
# macOS (Apple silicon)
CGO_ENABLED=1 GOOS=darwin  GOARCH=arm64 go build -o efwprobe     ./cmd/efwprobe

# Linux / Raspberry Pi (arm64)
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o efwprobe     ./cmd/efwprobe

# Windows (amd64)
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o efwprobe.exe ./cmd/efwprobe
```

## Run the tests

The suite drives a fake Transport, so it needs no hardware. The race detector
requires a C compiler; run it natively per OS, or cross-build a (non-race) test
binary to copy to the target.

```sh
# macOS — full suite with the race detector
CGO_ENABLED=1 go test -race ./efw/

# Linux — on the box (-race needs gcc/clang), or cross-build a test binary:
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go test -c -o efw.test     ./efw/

# Windows — on the box (-race needs a C compiler), or cross-build a test binary:
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test -c -o efw.test.exe ./efw/
```

## Linux permissions (udev)

`/dev/hidraw*` is root-only by default. Install a rule so the service user can
open the wheel:

```
# /etc/udev/rules.d/99-zwo-efw.rules
KERNEL=="hidraw*", ATTRS{idVendor}=="03c3", MODE="0660", TAG+="uaccess"
```

Then `sudo udevadm control --reload && sudo udevadm trigger`. Windows vendor-HID is
user-accessible — no driver install needed.

## `efwprobe` flags

| Flag | Effect |
|---|---|
| *(none)* | open the first wheel; print serial, alias, firmware, model, slots, position |
| `-list` | enumerate all attached EFWs by serial, then exit |
| `-serial <hex>` | open the wheel with that serial |
| `-goto <n>` | move to 0-based slot `n`, then watch it settle |
| `-uni` | use unidirectional moves (with `-goto`) |
| `-calibrate` | run the home / slot-realign routine |
| `-setalias <s>` | **persistent**: write a ≤8-char alias, read it back |
| `-aliastest <s>` | **persistent**: write alias → readback → clear → readback |
| `-watch` | poll status repeatedly |

## API sketch

```go
e, err := efw.OpenFirst()            // or efw.OpenBySerial("1f2120703dcef2b1")
defer e.Close()

pos, _ := e.Position()               // 0-based slot; -1 while moving
e.SetUnidirectional(true)            // host-side; affects subsequent moves
e.SetPosition(3)                     // initiate move; poll Position for completion
e.Calibrate()                        // home + realign

s, _   := e.SerialZWO()              // "1f2120703dcef2b1"
maj, min, _ := e.FirmwareVersion()   // 3, 9
model, _ := e.Model()                // "EFW-S-0"
code, _  := e.HWErrorCode()          // 0 = no error

wheels, _ := efw.List()              // enumerate with serials (multi-device)

custom := efw.New(myTransport, efw.DeviceInfo{FeatureLen: 64}) // supply your own transport
```

`Transport` is the seam: any backend implements `SetFeature`/`GetFeature`/`Close`
(with `buf[0]` = HID report ID). `efw.New` wraps an arbitrary `Transport` — used by
the platform openers, and by the end-to-end tests to back a real `*EFW` with a
fake transport. So the whole driver (and the Alpaca server above it) is testable
with no hardware and no cgo.

## Alpaca driver & end-to-end tests

The ASCOM Alpaca FilterWheel server built on this package is `asiefw` (in
`goalpaca_devices`). Its suite drives the full stack — Alpaca HTTP → server →
driver → this package → transport — against a fake wheel, with an optional
`EFW_HARDWARE=1` run against a real one.
