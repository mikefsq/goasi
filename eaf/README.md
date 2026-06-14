# eaf — pure-Go ZWO EAF (auto-focuser) driver

The `eaf` package of the `goasi` module (`github.com/mikefsq/goasi/eaf`): a
**cgo-free** driver for the ZWO EAF (Electronic Auto Focuser). It talks the
device's USB-HID feature-report protocol directly.  The EAF is a ZWO HID 
accessory and shares the EFW's framing (report ID `0x03` command / `0x01` 
reply, `7E 5A` signature), so it reuses the same HID `Transport` seam as 
`goasi/efw`.

## Layout

Paths are relative to the `goasi` module root (the parent of `eaf/`); run the
`go` commands below from there.

```
eaf/
  eaf.go               device logic + HID framing (pure Go, all platforms)
  transport.go         the Transport interface (the seam) + DeviceInfo
  transport_darwin.go  macOS   — IOKit (cgo)
  transport_linux.go   Linux   — hidraw ioctls (pure Go)
  transport_windows.go Windows — hid.dll + SetupAPI (pure Go)
  transport_stub.go    other   — compile-only
  eaf_test.go          unit tests over a fake Transport (no hardware)
cmd/eafprobe/          CLI to open / inspect / drive a focuser
```

Only the thin `transport_<os>.go` differs per platform; `eaf.go` and the tests are
platform-independent.

Unlike the EFW (a distinct opcode per command), the EAF has a **single control
report** (`0x03`) carrying the full writable state every time: each setter updates
one cached field and re-emits the whole report. Status/position come from the
query family (`0x02`).

## Status

| | |
|---|---|
| Coverage | Open handshake, status (position / maxstep / moving / state), absolute move, stop, reverse, beep, clear-error, firmware version, raw temperature, multi-device enumerate/list. |
| macOS / Linux / Windows | build + vet clean; `go test -race` green |
| Hardware | **WIP**  |
| Known gaps | Temperature is exposed **raw** (thermistor LUT pending); E-class focusers with `MaxStep > 65535` need a 24-bit move encoding that isn't implemented yet (`MoveTo` errors rather than truncating). |

## Build `eafprobe`

macOS uses IOKit (cgo); Linux and Windows are pure Go and cross-compile from any
host to a static binary.

```sh
# macOS (Apple silicon)
CGO_ENABLED=1 GOOS=darwin  GOARCH=arm64 go build -o eafprobe     ./cmd/eafprobe

# Linux / Raspberry Pi (arm64)
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o eafprobe     ./cmd/eafprobe

# Windows (amd64)
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o eafprobe.exe ./cmd/eafprobe
```

## Run the tests

The suite drives a fake Transport, so it needs no hardware. The race detector
requires a C compiler; run it natively per OS, or cross-build a (non-race) test
binary to copy to the target.

```sh
# macOS — full suite with the race detector
CGO_ENABLED=1 go test -race ./eaf/

# Linux — on the box (-race needs gcc/clang), or cross-build a test binary:
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go test -c -o eaf.test     ./eaf/

# Windows — on the box (-race needs a C compiler), or cross-build a test binary:
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test -c -o eaf.test.exe ./eaf/
```

## Linux permissions (udev)

`/dev/hidraw*` is root-only by default. Install a rule so the service user can
open the focuser:

```
# /etc/udev/rules.d/99-zwo-eaf.rules
KERNEL=="hidraw*", ATTRS{idVendor}=="03c3", MODE="0660", TAG+="uaccess"
```

Then `sudo udevadm control --reload && sudo udevadm trigger`. Windows vendor-HID is
user-accessible — no driver install needed.

## `eafprobe` flags

| Flag | Effect |
|---|---|
| *(none)* | open the first focuser; print PID, firmware, raw status, position, maxStep, moving, raw temperature |
| `-goto <n>` | absolute move to step `n`, then watch it settle |
| `-stop` | halt any in-progress motion |
| `-watch` | poll position + moving repeatedly |

## API sketch

```go
e, err := eaf.OpenFirst()            // or eaf.OpenAt(locationID) from List()
defer e.Close()

pos, _   := e.Position()             // current step (absolute; geared, no clutch)
max, _   := e.MaxStep()              // device-reported max travel
e.MoveTo(12000)                      // initiate move; poll Position/IsMoving for completion
moving, _ := e.IsMoving()
e.Stop()                             // halt

maj, min := e.FirmwareVersion()      // from the open handshake
raw, _   := e.TemperatureRaw()       // 16-bit thermistor field (LUT → °C pending)
e.SetReverse(true)                   // direction; persisted via the control report
e.SetBeep(false)
e.ClearError()                       // clear a latched error/limit

focusers, _ := eaf.List()            // enumerate with a snapshot per device

custom := eaf.New(myTransport, eaf.DeviceInfo{FeatureLen: 64}) // supply your own transport
```

`Transport` is the seam: any backend implements `SetFeature`/`GetFeature`/`Close`
(with `buf[0]` = HID report ID). `eaf.New` wraps an arbitrary `Transport` — used by
the platform openers, and by the end-to-end tests to back a real `*EAF` with a
fake transport. So the whole driver (and the Alpaca server above it) is testable
with no hardware and no cgo.

## Alpaca driver & end-to-end tests

The ASCOM Alpaca Focuser server built on this package is `asieaf` (in
`goalpaca_devices`). Its suite drives the full stack — Alpaca HTTP → server →
driver → this package → transport — against a fake focuser.
