// Command efwprobe validates the pure-Go EFW driver against a real wheel:
// enumerate+open, dump raw status, parse position, and (optionally) move.
//
//	efwprobe                 # read-only: open + dump status + parsed position
//	efwprobe -watch          # poll status every 500ms
//	efwprobe -goto 2         # move to slot 2 (0-based) and watch it settle
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mikefsq/goasi/efw"
)

func main() {
	gotoSlot := flag.Int("goto", -1, "move to 0-based slot, then watch it settle; -1 = read-only")
	uni := flag.Bool("uni", false, "use unidirectional moves (always same rotation direction)")
	calibrate := flag.Bool("calibrate", false, "run the home/realign routine, then watch it settle")
	setAlias := flag.String("setalias", "", "PERSISTENT: write a user alias (≤8 chars), then read it back")
	aliasTest := flag.String("aliastest", "", "PERSISTENT: write alias -> readback -> clear -> readback")
	watch := flag.Bool("watch", false, "poll status repeatedly")
	list := flag.Bool("list", false, "enumerate all attached EFWs (by serial) and exit")
	bindSerial := flag.String("serial", "", "open the EFW with this ZWO serial (hex) instead of the first")
	flag.Parse()

	if *list {
		wheels, err := efw.List()
		if err != nil {
			fmt.Fprintln(os.Stderr, "list:", err)
			os.Exit(1)
		}
		fmt.Printf("found %d EFW(s):\n", len(wheels))
		for i, w := range wheels {
			fmt.Printf("  [%d] loc=0x%08x pid=0x%04x serial=%s slots=%d\n",
				i, w.LocationID, w.PID, w.Serial, w.Slots)
		}
		return
	}

	var (
		e   *efw.EFW
		err error
	)
	if *bindSerial != "" {
		e, err = efw.OpenBySerial(*bindSerial)
	} else {
		e, err = efw.OpenFirst()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer e.Close()

	info := e.Info()
	fmt.Printf("opened EFW: product=%q usbSerial=%q featureLen=%d slots=%d\n",
		info.Product, info.Serial, e.FeatureLen(), e.Slots())
	if raw, hx, err := e.Serial(); err != nil {
		fmt.Println("serial     : error:", err)
	} else {
		fmt.Printf("serial(raw): % x   (hex=%s)\n", raw, hx)
	}
	if s, err := e.SerialZWO(); err != nil {
		fmt.Println("serial(ZWO): error:", err)
	} else {
		fmt.Printf("serial(ZWO): %s\n", s)
	}
	if raw, hx, err := e.Alias(); err != nil {
		fmt.Println("alias      : error:", err)
	} else {
		fmt.Printf("alias      : % x   (hex=%s)\n", raw, hx)
	}
	if hs, err := e.Handshake(); err != nil {
		fmt.Println("handshake  : error:", err)
	} else {
		fmt.Printf("handshake  : % x\n", clip(hs, 16))
	}
	if maj, min, err := e.FirmwareVersion(); err != nil {
		fmt.Println("firmware   : error:", err)
	} else {
		fmt.Printf("firmware   : %d.%d\n", maj, min)
	}
	if model, err := e.Model(); err != nil {
		fmt.Println("model      : error:", err)
	} else {
		fmt.Printf("model      : %q\n", model)
	}
	if code, err := e.HWErrorCode(); err != nil {
		fmt.Println("hw error   : error:", err)
	} else {
		fmt.Printf("hw error   : %d\n", code)
	}

	raw, err := e.RawStatus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "status:", err)
		os.Exit(1)
	}
	fmt.Printf("raw status : % x\n", clip(raw, 16))
	if pos, err := e.Position(); err != nil {
		fmt.Println("position   : error:", err)
	} else {
		fmt.Printf("position   : %d (byte7=0x%02x state byte4=0x%02x)\n", pos, raw[7], raw[4])
	}

	if *setAlias != "" {
		fmt.Printf("\nwriting alias %q (persistent) ...\n", *setAlias)
		if err := e.SetAlias([]byte(*setAlias)); err != nil {
			fmt.Fprintln(os.Stderr, "setalias:", err)
			os.Exit(1)
		}
		if raw, hx, err := e.Alias(); err != nil {
			fmt.Println("alias read-back: error:", err)
		} else {
			fmt.Printf("alias read-back: % x  (%q, hex=%s)\n", raw, string(raw), hx)
		}
	}

	if *aliasTest != "" {
		showAlias := func(label string) {
			raw, hx, err := e.Alias()
			if err != nil {
				fmt.Printf("  %-12s error: %v\n", label, err)
				return
			}
			fmt.Printf("  %-12s % x  (%q, hex=%s)\n", label, raw, string(raw), hx)
		}
		fmt.Println("\nalias test (persistent writes):")
		showAlias("before:")
		fmt.Printf("  write %q ...\n", *aliasTest)
		if err := e.SetAlias([]byte(*aliasTest)); err != nil {
			fmt.Fprintln(os.Stderr, "setalias:", err)
			os.Exit(1)
		}
		showAlias("after write:")
		fmt.Println("  clear (write zeros) ...")
		if err := e.ClearAlias(); err != nil {
			fmt.Fprintln(os.Stderr, "clearalias:", err)
			os.Exit(1)
		}
		showAlias("after clear:")
	}

	if *gotoSlot >= 0 {
		e.SetUnidirectional(*uni)
		fmt.Printf("\nmoving to slot %d (unidirectional=%v) ...\n", *gotoSlot, *uni)
		if err := e.SetPosition(*gotoSlot); err != nil {
			fmt.Fprintln(os.Stderr, "move:", err)
			os.Exit(1)
		}
		for i := 0; i < 100; i++ {
			r, _ := e.RawStatus()
			p, perr := e.Position()
			fmt.Printf("  t+%5dms  pos=%2d  raw=% x\n", (i+1)*300, p, clip(r, 12))
			if errors.Is(perr, efw.ErrWheelError) {
				fmt.Printf("wheel faulted: %v\n", perr)
				fmt.Println("  (unidirectional moves must wrap past the last slot; this firmware")
				fmt.Println("   faults on that wrap. try without -uni, then run -calibrate to reset.)")
				break
			}
			if p == *gotoSlot {
				fmt.Println("arrived.")
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	if *calibrate {
		fmt.Println("\ncalibrating (home + realign) ...")
		if err := e.Calibrate(); err != nil {
			fmt.Fprintln(os.Stderr, "calibrate:", err)
			os.Exit(1)
		}
		for i := 0; i < 100; i++ {
			r, _ := e.RawStatus()
			p, perr := e.Position()
			fmt.Printf("  t+%5dms  pos=%2d  raw=% x\n", (i+1)*300, p, clip(r, 12))
			if errors.Is(perr, efw.ErrWheelError) {
				fmt.Printf("wheel faulted: %v\n", perr)
				break
			}
			if p >= 0 && i > 0 {
				fmt.Println("settled.")
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	if *watch {
		fmt.Println("\nwatching (ctrl-c to stop):")
		for {
			r, _ := e.RawStatus()
			fmt.Printf("  raw=% x\n", clip(r, 16))
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func clip(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}
