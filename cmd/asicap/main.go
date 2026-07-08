// Command asisnap captures a single frame using the OFFICIAL ZWO SDK (via the cgo
// binding github.com/mikefsq/goasi/ccd), as a known-good reference against the
// pure-Go asicam driver. It opens the first camera, inits, sets a 1 s / gain 0
// exposure, snaps, and writes the RAW16 frame + pixel stats.
//
// Requires the ZWO SDK shared library resolvable at build+run time, e.g.:
//
//	cp .../ASI_Camera_SDK/.../lib/mac_arm64/libASICamera2.dylib /usr/local/lib/
//	# or: CGO_LDFLAGS="-L<sdk>/lib/mac_arm64" DYLD_LIBRARY_PATH=<sdk>/lib/mac_arm64
//
// Usage: asisnap [-cam 0] [-exposure 1s] [-gain 0] [-offset N] [-bin N] [-roi x,y,w,h] [-raw8] [-out f]
// The -offset/-bin/-roi/-raw8 controls mirror the pure-Go gosnap so the same invocation drives
// both for hardware diffs; it prints stdev (the read-noise metric the HCG dark-frame sweep needs).
package main

/*
// Exported by libASICamera2 but absent from the public header — the SDK's debug
// log (writes the full register/command trace to asicamerasdk.log).
extern int ASIEnableDebugLog(int iCameraID, int bEnable);
extern int ASIGetDebugLogPath(int iCameraID, char* path);
// The ccd binding's ASIGetVideoData wrapper is a no-op stub; call the SDK export directly.
extern int ASIGetVideoData(int iCameraID, unsigned char* pBuffer, long lBuffSize, int iWaitms);
*/
import "C"

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"
	"unsafe"

	"github.com/mikefsq/goasi/ccd"
)

// getVideoData calls the SDK's ASIGetVideoData directly (the ccd wrapper is a stub).
func getVideoData(asi *ccd.GoAsiCamera, buf []byte, waitMs int) int {
	if len(buf) == 0 {
		return -1
	}
	return int(C.ASIGetVideoData(C.int(asi.CameraID), (*C.uchar)(unsafe.Pointer(&buf[0])), C.long(len(buf)), C.int(waitMs)))
}

func main() {
	log.SetFlags(0)
	exposure := flag.Duration("exposure", 1*time.Second, "exposure time")
	gain := flag.Int("gain", 0, "gain (ASI units)")
	out := flag.String("out", "frame_sdk.fits", "output frame file; .fits/.fit writes FITS, any other extension writes raw RAW16")
	cam := flag.Int("cam", 0, "camera id to use")
	debug := flag.Bool("debug", false, "enable the SDK debug log (asicamerasdk.log)")
	list := flag.Bool("list", false, "open+list every camera (adds USB traffic; off for clean captures)")
	offset := flag.Int("offset", -1, "offset / black level (ASI Brightness); -1 = leave default")
	bin := flag.Int("bin", 1, "binning factor 1..4")
	roi := flag.String("roi", "", "sub-frame ROI as x,y,w,h in BINNED pixels; empty = full binned frame")
	raw8 := flag.Bool("raw8", false, "capture RAW8 (1 byte/pixel) instead of RAW16")
	cool := flag.Bool("cool", false, "run a 30s cooling test before the snap (default off = clean snap, matching gosnap)")
	nframes := flag.Int("n", 1, "with n>1: run a video-capture benchmark of N frames (ASIStartVideoCapture + ASIGetVideoData loop) and report fps")
	nsingle := flag.Int("nsingle", 0, "single-shot SOAK of N frames: SDK ARM-PER-FRAME path (SingleExposure + GetExposureBytes each frame), open-once and looped — the apples-to-apples A/B vs gosnap -soak. If the SDK's own single-shot runs clean where ours wedges, our arm sequence is buggy; if it ALSO wedges, arm-per-frame is inherently FX3-hostile (use video).")
	highspeed := flag.Bool("highspeed", false, "enable ASI_HIGH_SPEED_MODE (10-bit high-speed readout; implies RAW8)")
	fpsPerc := flag.Int("fps", 100, "ASI_BANDWIDTHOVERLOAD / FPS percent 40..100 — matches gosnap's -fps for A/B sweeps")
	flag.Parse()

	n := ccd.ASIGetNumOfConnectedCameras()
	fmt.Printf("connected cameras: %d\n", n)
	if n < 1 {
		log.Fatal("no ZWO cameras (plugged in? SDK dylib installed? ASIStudio disconnected?)")
	}
	if *list {
		for i := 0; i < n; i++ { // open+read each (adds USB traffic) — off by default
			c := &ccd.GoAsiCamera{CameraID: i}
			c.ASIGetCameraProperty()
			fmt.Printf("  [%d] %s\n", i, c.CameraInfo.Name)
		}
	}
	if *cam < 0 || *cam >= n {
		log.Fatalf("-cam %d out of range (0..%d)", *cam, n-1)
	}

	asi := &ccd.GoAsiCamera{CameraID: *cam}
	if rc := asi.ASIGetCameraProperty(); rc != 0 {
		log.Fatalf("ASIGetCameraProperty rc=%d", rc)
	}
	info := asi.CameraInfo
	fmt.Printf("camera : %s  %dx%d  %d-bit  color=%v  usb3cam=%v\n",
		info.Name, info.MaxWidth, info.MaxHeight, info.BitDepth, info.IsColorCam, info.IsUSB3Camera)
	fmtName := map[int]string{0: "RAW8", 1: "RGB24", 2: "RAW16", 3: "Y8"}
	var fmts []string
	for _, f := range info.SupportedVideoFormat {
		if n, ok := fmtName[f]; ok {
			fmts = append(fmts, n)
		} else {
			fmts = append(fmts, fmt.Sprintf("fmt%d", f))
		}
	}
	fmt.Printf("modes  : bins=%v  formats=%v  cooler=%v  trigger=%v\n",
		info.SupportedBins, fmts, info.IsCoolerCam, info.IsTriggerCam)

	if rc := asi.ASIOpenCamera(); rc != 0 {
		log.Fatalf("ASIOpenCamera rc=%d", rc)
	}
	defer asi.ASICloseCamera()

	if *debug {
		if rc := int(C.ASIEnableDebugLog(C.int(*cam), 1)); rc != 0 {
			fmt.Printf("WARN ASIEnableDebugLog rc=%d\n", rc)
		} else {
			var path [1024]C.char
			C.ASIGetDebugLogPath(C.int(*cam), &path[0])
			fmt.Printf("SDK debug log enabled -> %s\n", C.GoString(&path[0]))
		}
	}

	if rc := asi.ASIInitCamera(); rc != 0 {
		log.Fatalf("ASIInitCamera rc=%d", rc)
	}

	if sn := asi.ASIGetSerialNumber(); sn != "" {
		fmt.Printf("serial : %x\n", []byte(sn))
	}
	if *cool {
		// Optional cooling run: read ambient, target 10 °C below it, cooler on, watch temp +
		// TEC power for ~30 s, then cooler off. (Off by default so the snap is clean/comparable.)
		asi.ASISetControlValue(ccd.ASI_TARGET_TEMP, 40, 0)
		asi.ASISetControlValue(ccd.ASI_COOLER_ON, 1, 0)
		time.Sleep(800 * time.Millisecond)
		_, amb, _ := asi.ASIGetControlValue(ccd.ASI_TEMPERATURE)
		tgt := int(amb)/10 - 10
		fmt.Printf("ambient: %.1f °C -> target %d °C; cooling for 30s\n", float64(amb)/10, tgt)
		asi.ASISetControlValue(ccd.ASI_TARGET_TEMP, tgt, 0)
		for i := 0; i < 15; i++ {
			time.Sleep(2 * time.Second)
			_, temp, _ := asi.ASIGetControlValue(ccd.ASI_TEMPERATURE)
			_, pw, _ := asi.ASIGetControlValue(ccd.ASI_COOLER_POWER_PERC)
			fmt.Printf("  t=%2ds  temp %.1f °C  power %d%%\n", (i+1)*2, float64(temp)/10, pw)
		}
		asi.ASISetControlValue(ccd.ASI_COOLER_ON, 0, 0)
		asi.ASISetControlValue(ccd.ASI_TARGET_TEMP, 40, 0)
	}

	// Output depth, binning, ROI window, offset — mirror gosnap's controls via the SDK.
	imgType := ccd.ASI_IMG_RAW16
	if *raw8 || *highspeed {
		imgType = ccd.ASI_IMG_RAW8 // high-speed is 10-bit → RAW8
	}
	hsVal := 0
	if *highspeed {
		hsVal = 1
	}
	asi.SetHighSpeedMode(hsVal) // set EXPLICITLY both ways — the camera retains this control across opens
	fmt.Printf("ASI_HIGH_SPEED_MODE = %d\n", hsVal)
	if *bin < 1 {
		*bin = 1
	}
	x, y, w, h := 0, 0, info.MaxWidth/(*bin), info.MaxHeight/(*bin)
	if *roi != "" {
		if _, e := fmt.Sscanf(*roi, "%d,%d,%d,%d", &x, &y, &w, &h); e != nil {
			log.Fatalf("bad -roi %q (want x,y,w,h): %v", *roi, e)
		}
	}
	if rc := asi.ASISetROIFormat(w, h, *bin, imgType); rc != 0 {
		log.Fatalf("ASISetROIFormat rc=%d", rc)
	}
	if x != 0 || y != 0 {
		if rc := asi.ASISetStartPos(x, y); rc != 0 {
			log.Fatalf("ASISetStartPos rc=%d", rc)
		}
	}
	asi.SetExposure(*exposure)
	asi.SetGain(*gain)
	if *offset >= 0 {
		asi.ASISetControlValue(ccd.ASI_OFFSET, *offset, 0)
	}
	asi.ASISetControlValue(ccd.ASI_BANDWIDTHOVERLOAD, *fpsPerc, 0) // USB bandwidth — matches gosnap's -fps for a fair benchmark

	// Single-shot SOAK (nsingle>0): the SDK's ARM-PER-FRAME path — SingleExposure (ASIStartExposure
	// + status poll) then GetExposureBytes (ASIGetDataAfterExp) every frame, open-once and looped.
	// The true A/B vs gosnap -soak: does the SDK's OWN single-shot path wedge the FX3 too?
	if *nsingle > 0 {
		step := ccd.BytesPerPixel(imgType)
		fmt.Printf("SDK single-shot soak: %d frames at %dx%d %d-bit, exp %s (ARM EVERY FRAME)...\n", *nsingle, w, h, step*8, *exposure)
		ok, fails := 0, 0
		t0 := time.Now()
		for f := 0; f < *nsingle; f++ {
			if rc := asi.SingleExposure(); rc != 0 {
				fails++
				fmt.Printf("[%6.1fs] frame %d SingleExposure FAILED rc=%d\n", time.Since(t0).Seconds(), f, rc)
				continue
			}
			rc, frame := asi.GetExposureBytes()
			if rc != 0 || len(frame.Pixels) == 0 {
				fails++
				fmt.Printf("[%6.1fs] frame %d GetExposureBytes FAILED rc=%d len=%d\n", time.Since(t0).Seconds(), f, rc, len(frame.Pixels))
				continue
			}
			ok++
			if f%200 == 0 {
				fmt.Printf("[%6.1fs] frame %d ok\n", time.Since(t0).Seconds(), f)
			}
		}
		dt := time.Since(t0).Seconds()
		fmt.Printf("\n*** SDK SINGLE-SHOT *** %d/%d ok, %d failed, %.1f fps (%.1fs)\n", ok, *nsingle, fails, float64(ok)/dt, dt)
		return
	}

	// Video-capture benchmark (n>1): the SDK's high-fps path — ASIStartVideoCapture then a
	// tight ASIGetVideoData loop, the apples-to-apples comparison for gosnap's .ser burst.
	if *nframes > 1 {
		step := ccd.BytesPerPixel(imgType)
		fbSize := w * h * step
		waitMs := int((*exposure).Milliseconds())*2 + 500
		writeSER := strings.HasSuffix(strings.ToLower(*out), ".ser")
		var sw *serWriter
		if writeSER {
			bayer := map[int]string{0: "RGGB", 1: "BGGR", 2: "GRBG", 3: "GBRG"}[info.BayerPattern]
			var serr error
			if sw, serr = newSER(*out, w, h, step, serColorID(info.IsColorCam, bayer), "ZWO "+info.Name); serr != nil {
				log.Fatal(serr)
			}
		}
		fmt.Printf("video benchmark: %d frames at %dx%d %d-bit, exp %s (writeSER=%v)...\n", *nframes, w, h, step*8, *exposure, writeSER)
		if rc := asi.ASIStartVideoCapture(); rc != 0 {
			log.Fatalf("ASIStartVideoCapture rc=%d", rc)
		}
		warm := make([]byte, fbSize)
		getVideoData(asi, warm, waitMs) // warm-up frame (discarded), matches gosnap's frame-0 arm
		ok := 0
		var t0 time.Time
		if writeSER {
			// Async double-buffered writer — the SAME design as gosnap, so the SDK client also
			// gets disk overlap. This measures capture under destination impedance on both sides.
			const pool = 4
			free := make(chan []byte, pool)
			queue := make(chan []byte, pool)
			for i := 0; i < pool; i++ {
				free <- make([]byte, fbSize)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				for fr := range queue {
					_ = sw.writeFrame(fr)
					free <- fr[:cap(fr)]
				}
			}()
			t0 = time.Now()
			for f := 0; f < *nframes; f++ {
				fb := <-free
				if getVideoData(asi, fb, waitMs) == 0 {
					ok++
					queue <- fb
				} else {
					free <- fb
				}
			}
			close(queue)
			<-done
			dt := time.Since(t0).Seconds()
			asi.ASIStopVideoCapture()
			sw.close()
			_, dropped := asi.ASIGetDroppedFrames()
			fmt.Printf("\n*** SDK VIDEO+SER *** %d/%d frames -> %s  %dx%d %d-bit  %.1f fps (%.3fs)  dropped=%d\n",
				ok, *nframes, *out, w, h, step*8, float64(ok)/dt, dt, dropped)
			return
		}
		buf := make([]byte, fbSize)
		ivs := make([]float64, 0, *nframes)
		t0 = time.Now()
		last := t0
		for f := 0; f < *nframes; f++ {
			if getVideoData(asi, buf, waitMs) == 0 {
				ok++
			}
			now := time.Now()
			ivs = append(ivs, float64(now.Sub(last).Microseconds())/1000.0)
			last = now
		}
		dt := time.Since(t0).Seconds() // stamped before StopVideoCapture (teardown excluded)
		asi.ASIStopVideoCapture()
		_, dropped := asi.ASIGetDroppedFrames()
		fmt.Printf("\n*** SDK VIDEO *** %d/%d frames  %dx%d %d-bit  %.1f fps (%.3fs)  dropped=%d\n",
			ok, *nframes, w, h, step*8, float64(ok)/dt, dt, dropped)
		if len(ivs) > 2 {
			s := append([]float64(nil), ivs...)
			sort.Float64s(s)
			med := s[len(s)/2]
			var gaps int
			for _, v := range ivs {
				if v > 1.5*med {
					gaps++
				}
			}
			fmt.Printf("  intervals(ms): med=%.2f p1=%.2f p99=%.2f max=%.2f | >1.5×med (likely drops)=%d/%d\n",
				med, s[len(s)/100], s[len(s)*99/100], s[len(s)-1], gaps, len(ivs))
			if *out != "" {
				var b strings.Builder
				for _, v := range ivs {
					fmt.Fprintf(&b, "%.3f\n", v)
				}
				if werr := os.WriteFile(*out, []byte(b.String()), 0o644); werr == nil {
					fmt.Printf("  wrote %d per-frame intervals -> %s\n", len(ivs), *out)
				}
			}
		}
		return
	}

	fmt.Printf("exposing %s (gain %d, offset %d, bin %d, %dx%d, %d-bit out)...\n",
		*exposure, *gain, *offset, *bin, w, h, ccd.BytesPerPixel(imgType)*8)
	if rc := asi.SingleExposure(); rc != 0 {
		log.Fatalf("SingleExposure rc=%d", rc)
	}
	rc, frame := asi.GetExposureBytes()
	if rc != 0 {
		log.Fatalf("GetExposureBytes rc=%d", rc)
	}

	step := ccd.BytesPerPixel(imgType)
	if step == 2 {
		if err := writeFrameFile(*out, frame.Pixels, frame.Width, frame.Height, info, (*exposure).Seconds(), *gain); err != nil {
			log.Fatal(err)
		}
	} else { // RAW8 — the FITS writer is 16-bit; dump raw
		fmt.Printf("(RAW8 — FITS writer is 16-bit; writing raw)\n")
		if err := os.WriteFile(*out, frame.Pixels, 0o644); err != nil {
			log.Fatal(err)
		}
	}

	// Pixel stats + STDEV (read-noise; the HCG dark-frame sweep metric) — matches gosnap.
	mn, mx, cnt, sum, sumsq := 1<<16, 0, 0, 0.0, 0.0
	for i := 0; i+step <= len(frame.Pixels); i += step {
		v := int(frame.Pixels[i])
		if step == 2 {
			v |= int(frame.Pixels[i+1]) << 8
		}
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
		sum += float64(v)
		sumsq += float64(v) * float64(v)
		cnt++
	}
	avg, sd := 0.0, 0.0
	if cnt > 0 {
		avg = sum / float64(cnt)
		if va := sumsq/float64(cnt) - avg*avg; va > 0 {
			sd = math.Sqrt(va)
		}
	}
	fmt.Printf("OK wrote %d bytes -> %s  %dx%d, %d-bit out  pixels: min=%d max=%d avg=%.1f stdev=%.2f\n",
		len(frame.Pixels), *out, frame.Width, frame.Height, step*8, mn, mx, avg, sd)
}
