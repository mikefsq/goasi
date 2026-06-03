# goasi

cgo bindings for the [ZWO](https://www.zwoastro.com/) ASI device SDKs, as a set
of small, self-contained Go packages — one per device family.

Module path: `github.com/mikefsq/goasi`

| Package | Device | ZWO SDK | Header | Links |
|---|---|---|---|---|
| `ccd` | Camera | ASICamera2 (V1.41) | `ASICamera2.h` | `-lASICamera2` |
| `caa` | Rotator (Camera Angle Adjuster) | CAA (V1.5.9) | `CAA_API.h` | `-lCAA` |
| `eaf` | Focuser (Electronic Auto Focuser) | EAF (V1.8.1) | `EAF_focuser.h` | `-lEAFFocuser` |
| `efw` | Filter wheel (Electronic Filter Wheel) | EFW (V1.8.4) | `EFW_filter.h` | `-lEFWFilter` |

Each package is independent: import only the one(s) you need. Drivers in this
workspace (`asiccd`, `asieaf`, `asiefw`, `asicaa`) each import a single package.

## The shared libraries are not bundled

The ZWO `.so`/`.dylib` runtime libraries are proprietary and **not** redistributed
here — only the headers, which are all that's needed to compile. Download the ASI
SDK for your device from ZWO and install the matching-architecture library
yourself.

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

**Supported targets** follow the ZWO SDK: Linux (`x86`/`x64`/`armv6,7,8`) and
macOS (`x86_64` and `arm64`). On Linux the EAF additionally needs
`libsdbus-c++.so.2` and `libWrapperSdbus.so` from the same lib directory.

## Layout

```
goasi/
├── ccd/   ccd.go   + include/ASICamera2.h
├── caa/   caa.go   + include/CAA_API.h
├── eaf/   eaf.go   + include/EAF_focuser.h
└── efw/   efw.go   + include/EFW_filter.h
```

Each package's cgo preamble uses `-I${SRCDIR}/include`, so the bundled header
resolves correctly even when the package is imported as a dependency.

## Notes

- **EAF header is C++.** `EAF_focuser.h` declares a default argument
  (`EAFStopAndWait(..., int timeoutMs = 1000)`), which is not valid C, so it
  cannot be `#include`d in a cgo C preamble. `eaf.go` therefore declares the
  C-ABI prototypes and the `EAF_INFO` layout inline (the ZWO library exports them
  with C linkage); the header is kept in `eaf/include` for reference. Keep the
  inline declarations in sync if you wrap more functions.
- **Scope.** `ccd` is a full camera binding; `caa`/`eaf`/`efw` cover the core
  surface (enumerate/open/close, properties, the primary motion/position
  operations, temperature, reverse/beep, firmware, serial). Peripheral features
  (EAF Bluetooth/battery, EFW HW-error, control-caps enumeration) are not yet
  wrapped — extend per binding as needed.
- **Arch constraint.** The bindings build for the arches the installed library
  provides; there is no `//go:build` arch restriction (the camera's old
  `!arm64` workaround was removed once V1.41 shipped `mac_arm64`).
