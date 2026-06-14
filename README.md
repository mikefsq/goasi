# goasi

Go packages for [ZWO](https://www.zwoastro.com/) ASI devices — one per device
family. The camera and rotator are **cgo bindings** to ZWO's ASI SDKs; the
focuser and filter wheel are **pure-Go USB-HID drivers** with no SDK runtime
dependency.

Module path: `github.com/mikefsq/goasi`

| Package | Device | Kind | SDK / transport |
|---|---|---|---|
| `ccd` | Camera | cgo SDK binding | ASICamera2 (V1.41), `-lASICamera2` |
| `caa` | Rotator (Camera Angle Adjuster) | cgo SDK binding | CAA (V1.5.9), `-lCAA` |
| `eaf` | Focuser (Electronic Auto Focuser) | **pure-Go USB-HID** | no SDK — see [`eaf/README.md`](eaf/README.md) |
| `efw` | Filter wheel (Electronic Filter Wheel) | **pure-Go USB-HID** | no SDK — see [`efw/README.md`](efw/README.md) |

Each package is independent: import only the one(s) you need. Drivers in the
companion workspace (`asiccd`, `asieaf`, `asiefw`, `asicaa`) each import a single
package.

The two pure-Go HID drivers (`eaf`, `efw`) cross-compile to a static binary for
Linux and Windows and need no vendor library — only the IOKit framework on macOS.
Their own READMEs cover build, tests, and Linux udev permissions. The rest of
this document concerns the two **cgo SDK** packages (`ccd`, `caa`).

## The shared libraries are not bundled

The ZWO `.so`/`.dylib` runtime libraries for `ccd`/`caa` are not redistributed
here — only the headers, which are all that's needed to compile. Download the ASI
SDK for your device from ZWO and install the matching-architecture library
yourself. (`eaf`/`efw` need none of this.)

The packages link with `-L/usr/local/lib`, so the simplest setup is to drop the
library there:

```sh
sudo cp libASICamera2.* /usr/local/lib/     # the lib(s) for your device + arch
CGO_ENABLED=1 go build ./...
```

To keep the SDK elsewhere, point the linker (and the loader) at it:

```sh
CGO_ENABLED=1 CGO_LDFLAGS="-L/path/to/sdk/lib" go build ./...
LD_LIBRARY_PATH=/path/to/sdk/lib ./yourprogram      # Linux
DYLD_LIBRARY_PATH=/path/to/sdk/lib ./yourprogram    # macOS
```

### macOS: rewrite the library path baked into the binary

On macOS the library's path is recorded in the binary at link time (the dylib's
install name). If the loader can't find the library at runtime, you don't need
`DYLD_LIBRARY_PATH` — just rewrite the recorded path in the binary with
`install_name_tool`:

```sh
otool -L ./asiccd                                   # show the recorded dylib path
install_name_tool -change libASICamera2.dylib \
    /opt/zwo/libASICamera2.dylib ./asiccd           # repoint it to the real file
```

If the install name uses `@rpath`, add a search path instead:
`install_name_tool -add_rpath /opt/zwo ./asiccd`.

**Supported targets** for the cgo packages follow the ZWO SDK: Linux
(`x86`/`x64`/`armv6,7,8`) and macOS (`x86_64` and `arm64`). On Linux the EAF SDK
historically also needed `libsdbus-c++.so.2` / `libWrapperSdbus.so`; the pure-Go
`eaf` driver removes that dependency entirely.

## Layout

```
goasi/
├── ccd/   ccd.go  + include/ASICamera2.h   — cgo, ZWO ASICamera2 SDK
├── caa/   caa.go  + include/CAA_API.h      — cgo, ZWO CAA SDK
├── eaf/   eaf.go  + transport_*.go         — pure-Go USB-HID driver
└── efw/   efw.go  + transport_*.go         — pure-Go USB-HID driver
```

The cgo packages (`ccd`, `caa`) use `-I${SRCDIR}/include` in their cgo preamble,
so the bundled header resolves correctly even when the package is imported as a
dependency. The HID packages (`eaf`, `efw`) have only a thin per-OS
`transport_<os>.go` and are otherwise platform-independent.

## Notes

- **Pure-Go HID drivers.** `eaf` and `efw` talk the device's USB-HID
  feature-report protocol directly — no vendor SDK in the process. See their
  package READMEs for the device surface, hardware-validation status, and the
  Linux udev rule.
- **Coverage.** `ccd` is a full camera binding; `caa` wraps the CAA SDK surface
  with one exception — `CAAMinDegree`, which the shipped `libCAA` exports with
  C++ name mangling instead of C linkage (a ZWO SDK bug), so it is not callable
  from cgo.

## License

The Go binding code in this repository is licensed under the
[MIT License](LICENSE).

This does **not** extend to the ZWO ASI SDK. The headers under `ccd/include/` and
`caa/include/` are © ZWO, and the runtime libraries (not included here) are ZWO's
property under ZWO's own terms — obtain the SDK from
[ZWO](https://www.zwoastro.com/). The MIT license covers only the Go code in this
repository.
