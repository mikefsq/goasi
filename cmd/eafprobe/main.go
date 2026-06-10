// Command eafprobe exercises the pure-Go ZWO EAF driver against a real focuser:
// open, dump status (position/maxstep/moving/temp/firmware), and optionally move.
//
//	eafprobe                # read-only: open + dump status
//	eafprobe -goto 12000    # absolute move, then watch it settle
//	eafprobe -stop          # halt motion
//	eafprobe -watch         # poll position+moving repeatedly
//
// NOTE: reverse-engineered from libEAFFocuser, not yet hardware-validated. The
// temperature is raw (thermistor LUT pending); E-class focusers (MaxStep > 65535)
// need a 24-bit move encoding that isn't decoded yet.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mikefsq/goasi/eaf"
)

func main() {
	gotoStep := flag.Int("goto", -1, "absolute move to step, then watch; -1 = read-only")
	stop := flag.Bool("stop", false, "halt motion")
	watch := flag.Bool("watch", false, "poll position+moving repeatedly")
	flag.Parse()

	e, err := eaf.OpenFirst()
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer e.Close()

	info := e.Info()
	maj, min := e.FirmwareVersion()
	fmt.Printf("opened ZWO EAF: PID=0x%04x loc=0x%x fw=%d.%d\n", info.PID, info.LocationID, maj, min)
	if r, err := e.Status(); err == nil {
		fmt.Printf("status raw : % x\n", r)
	}
	if p, err := e.Position(); err == nil {
		fmt.Printf("position   : %d\n", p)
	}
	if m, err := e.MaxStep(); err == nil {
		fmt.Printf("maxStep    : %d\n", m)
	}
	if mv, err := e.IsMoving(); err == nil {
		fmt.Printf("moving     : %v\n", mv)
	}
	if t, err := e.TemperatureRaw(); err == nil {
		fmt.Printf("temp (raw) : %d (thermistor LUT pending hardware)\n", t)
	}

	switch {
	case *stop:
		if err := e.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, "stop:", err)
		}
	case *gotoStep >= 0:
		fmt.Printf("\nmoving to %d...\n", *gotoStep)
		if err := e.MoveTo(*gotoStep); err != nil {
			fmt.Fprintln(os.Stderr, "moveto:", err)
			os.Exit(1)
		}
		watchSettle(e)
	case *watch:
		fmt.Println("\nwatching (Ctrl-C to stop)...")
		for {
			p, _ := e.Position()
			m, _ := e.IsMoving()
			fmt.Printf("position=%d moving=%v\n", p, m)
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func watchSettle(e *eaf.EAF) {
	for i := 0; i < 120; i++ {
		m, err := e.IsMoving()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ismoving:", err)
			return
		}
		if !m {
			p, _ := e.Position()
			fmt.Printf("settled at %d\n", p)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println("gave up waiting for the focuser to settle")
}
