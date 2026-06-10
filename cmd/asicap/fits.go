package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mikefsq/goasi/ccd"
)

// writeFrameFile saves a captured frame as FITS when the path ends in .fits/.fit
// (viewable directly in any astronomy tool), otherwise the raw RAW16 little-endian
// bytes. Metadata (Bayer pattern, pixel size, model) comes from the SDK CameraInfo.
func writeFrameFile(path string, data []byte, width, height int, info ccd.AsiCameraInfo, exposureSec float64, gain int) error {
	l := strings.ToLower(path)
	if strings.HasSuffix(l, ".fits") || strings.HasSuffix(l, ".fit") {
		bayer := ""
		if info.IsColorCam {
			bayer = bayerString(info.BayerPattern)
		}
		return writeFITS(path, data, width, height, bayer, exposureSec, info.PixelSize, gain, info.Name)
	}
	return os.WriteFile(path, data, 0o644)
}

// bayerString maps the ASI_BAYER_* enum to the CFA string FITS readers expect.
func bayerString(p int) string {
	switch p {
	case ccd.ASI_BAYER_RG:
		return "RGGB"
	case ccd.ASI_BAYER_BG:
		return "BGGR"
	case ccd.ASI_BAYER_GR:
		return "GRBG"
	case ccd.ASI_BAYER_GB:
		return "GBRG"
	}
	return ""
}

// writeFITS writes a single 16-bit image as a FITS primary HDU that any astronomy
// viewer (ASIStudio, PixInsight, DS9, AstroImageJ) opens directly.
//
// The camera delivers RAW16 little-endian UNSIGNED samples. FITS BITPIX=16 is SIGNED
// big-endian, so we use the universal unsigned-16 convention — store (v - 32768) as
// signed and advertise BZERO=32768/BSCALE=1 so readers reconstruct the original value.
// Subtracting 32768 from a 16-bit value is exactly toggling its top bit, so per sample
// the conversion is: swap to big-endian and XOR 0x80 into the high byte.
//
// bayer is "" for mono; for a color sensor pass the CFA pattern (e.g. "RGGB") so the
// viewer debayers. Rows are written sensor order (top first) with ROWORDER=TOP-DOWN.
func writeFITS(path string, data []byte, width, height int, bayer string, exposureSec, pixUm float64, gain int, model string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)

	cards := []string{
		fitsLogical("SIMPLE", true, "conforms to FITS standard"),
		fitsInt("BITPIX", 16, "16-bit; BZERO/BSCALE -> unsigned"),
		fitsInt("NAXIS", 2, ""),
		fitsInt("NAXIS1", width, "image width"),
		fitsInt("NAXIS2", height, "image height"),
		fitsNum("BZERO", "32768", "offset for unsigned 16-bit"),
		fitsNum("BSCALE", "1", ""),
	}
	if exposureSec > 0 {
		cards = append(cards, fitsNum("EXPTIME", trim(exposureSec), "[s] exposure time"))
	}
	cards = append(cards, fitsInt("GAIN", gain, "gain (ASI 0.1 dB units)"))
	if model != "" {
		cards = append(cards, fitsStr("INSTRUME", model, "camera"))
	}
	if pixUm > 0 {
		cards = append(cards, fitsNum("XPIXSZ", trim(pixUm), "[um] pixel size"))
		cards = append(cards, fitsNum("YPIXSZ", trim(pixUm), "[um] pixel size"))
	}
	if bayer != "" {
		cards = append(cards, fitsStr("BAYERPAT", bayer, "Bayer matrix"))
		cards = append(cards, fitsInt("XBAYROFF", 0, ""))
		cards = append(cards, fitsInt("YBAYROFF", 0, ""))
	}
	cards = append(cards, fitsStr("ROWORDER", "TOP-DOWN", "sensor row order"))
	cards = append(cards, fmt.Sprintf("%-80s", "END"))

	hdr := strings.Join(cards, "")
	if pad := (2880 - len(hdr)%2880) % 2880; pad > 0 {
		hdr += strings.Repeat(" ", pad)
	}
	if _, err := w.WriteString(hdr); err != nil {
		return err
	}

	// Byte-swap to big-endian and flip the sign bit (the -32768 offset), in 1 MiB chunks.
	tmp := make([]byte, 1<<20)
	for off := 0; off < len(data); off += len(tmp) {
		end := off + len(tmp)
		if end > len(data) {
			end = len(data)
		}
		m := end - off
		for i := 0; i+1 < m; i += 2 {
			tmp[i] = data[off+i+1] ^ 0x80 // high byte, sign-flipped
			tmp[i+1] = data[off+i]        // low byte
		}
		if _, err := w.Write(tmp[:m]); err != nil {
			return err
		}
	}
	if pad := (2880 - len(data)%2880) % 2880; pad > 0 {
		if _, err := w.Write(make([]byte, pad)); err != nil {
			return err
		}
	}
	return w.Flush()
}

// FITS header cards are exactly 80 bytes: KEYWORD (<=8, left-justified) + "= " + a
// value field, optionally " / comment", space-padded to 80.
func fitsCard(keyword, valueField, comment string) string {
	card := fmt.Sprintf("%-8s= %s", keyword, valueField)
	if comment != "" {
		card += " / " + comment
	}
	if len(card) > 80 {
		card = card[:80]
	}
	return card + strings.Repeat(" ", 80-len(card))
}

func fitsInt(k string, v int, c string) string { return fitsCard(k, fmt.Sprintf("%20d", v), c) }
func fitsNum(k, v, c string) string            { return fitsCard(k, fmt.Sprintf("%20s", v), c) }
func fitsLogical(k string, b bool, c string) string {
	s := "F"
	if b {
		s = "T"
	}
	return fitsCard(k, fmt.Sprintf("%20s", s), c)
}

// fitsStr writes a quoted string value (FITS requires >= 8 chars inside the quotes).
func fitsStr(k, v, c string) string {
	if len(v) < 8 {
		v += strings.Repeat(" ", 8-len(v))
	}
	return fitsCard(k, fmt.Sprintf("'%s'", v), c)
}

// trim formats a float without trailing-zero / exponent noise for header readability.
func trim(v float64) string { return fmt.Sprintf("%g", v) }
