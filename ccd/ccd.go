package ccd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"time"
	"unsafe"

	fitsio "github.com/astrogo/fitsio"
)

// Requires cgo: CGO_ENABLED=1 go build
// Supported targets follow the ZWO SDK: linux (x86/x64/armv6,7,8) and macOS
// (x86_64 and arm64). Pick the matching arch's library from the ZWO SDK.
//
// The ZWO ASICamera2 shared library is NOT bundled with this module — install it
// yourself from the ZWO SDK. The headers in ./include are sufficient to compile;
// the library must be resolvable by the linker and at runtime, e.g.:
//	install libASICamera2.{so,dylib} into /usr/local/lib, or
//	CGO_LDFLAGS="-L/path/to/sdk/lib" go build   (build time), and
//	LD_LIBRARY_PATH / DYLD_LIBRARY_PATH=/path/to/sdk/lib   (run time)

/*
#cgo CFLAGS: -I${SRCDIR}/include -g -Wall
#cgo darwin LDFLAGS: -L/usr/local/lib -lASICamera2
#cgo linux  LDFLAGS: -L/usr/local/lib -lASICamera2
#include <stdlib.h>
#include  <ASICamera2.h>
*/
import "C"

const (
	ASI_BAYER_RG int = iota
	ASI_BAYER_BG
	ASI_BAYER_GR
	ASI_BAYER_GB
)

const (
	ASI_IMG_RAW8 int = iota
	ASI_IMG_RGB24
	ASI_IMG_RAW16
	ASI_IMG_Y8
)

const (
	ASI_GUIDE_NORTH int = iota
	ASI_GUIDE_SOUTH
	ASI_GUIDE_EAST
	ASI_GUIDE_WEST
)

const (
	ASI_FLIP_NONE int = iota
	ASI_FLIP_HORIZ
	ASI_FLIP_VERT
	ASI_FLIP_BOTH
)

const (
	ASI_MODE_NORMAL int = iota
	ASI_MODE_TRIG_SOFT_EDGE
	ASI_MODE_TRIG_RISE_EDGE
	ASI_MODE_TRIG_FALL_EDGE
	ASI_MODE_TRIG_SOFT_LEVEL
	ASI_MODE_TRIG_HIGH_LEVEL
	ASI_MODE_TRIG_LOW_LEVEL
)

const (
	ASI_TRIG_OUTPUT_PINA int = iota
	ASI_TRIG_OUTPUT_PINB
)

const (
	ASI_FALSE = 0
	ASI_TRUE  = 1
)

const (
	ASI_EXP_IDLE int = iota
	ASI_EXP_WORKING
	ASI_EXP_SUCCESS
	ASI_EXP_FAILED
)

const (
	ASI_SUCCESS                    int = iota
	ASI_ERROR_INVALID_INDEX            //no camera connected or index value out of boundary
	ASI_ERROR_INVALID_ID               //invalid ID
	ASI_ERROR_INVALID_CONTROL_TYPE     //invalid control type
	ASI_ERROR_CAMERA_CLOSED            //camera didn't open
	ASI_ERROR_CAMERA_REMOVED           //failed to find the camera, maybe the camera has been removed
	ASI_ERROR_INVALID_PATH             //cannot find the path of the file
	ASI_ERROR_INVALID_FILEFORMAT       //
	ASI_ERROR_INVALID_SIZE             //wrong video format size
	ASI_ERROR_INVALID_IMGTYPE          //unsupported image formate
	ASI_ERROR_OUTOF_BOUNDARY           //the startpos is out of boundary
	ASI_ERROR_TIMEOUT                  //timeout
	ASI_ERROR_INVALID_SEQUENCE         //stop capture first
	ASI_ERROR_BUFFER_TOO_SMALL         //buffer size is not big enough
	ASI_ERROR_VIDEO_MODE_ACTIVE        //
	ASI_ERROR_EXPOSURE_IN_PROGRESS     //
	ASI_ERROR_GENERAL_ERROR            //general error, eg: value is out of valid range
	ASI_ERROR_INVALID_MODE             //the current mode is wrong
)

func ASI_Error_Code_Message(code int) string {
	var err_str []string
	err_str = append(err_str, "success")
	err_str = append(err_str, "no camera connected or index value out of boundary")
	err_str = append(err_str, "invalid ID")
	err_str = append(err_str, "invalid control type")
	err_str = append(err_str, "camera didn't open")
	err_str = append(err_str, "failed to find the camera, maybe the camera has been removed")
	err_str = append(err_str, "cannot find the path of the file")
	err_str = append(err_str, "invalid filefornat")
	err_str = append(err_str, "wrong video format size")
	err_str = append(err_str, "unsupported image formate")
	err_str = append(err_str, "the startpos is out of boundary")
	err_str = append(err_str, "timeout")
	err_str = append(err_str, "stop capture first")
	err_str = append(err_str, "buffer size is not big enough")
	err_str = append(err_str, "video mode active")
	err_str = append(err_str, "exposure in progress")
	err_str = append(err_str, "general error, eg: value is out of valid range")
	err_str = append(err_str, "the current mode is wrong")
	return err_str[code]
}

const (
	ASI_GAIN int = iota
	ASI_EXPOSURE
	ASI_GAMMA
	ASI_WB_R
	ASI_WB_B
	ASI_OFFSET
	ASI_BANDWIDTHOVERLOAD
	ASI_OVERCLOCK
	ASI_TEMPERATURE
	ASI_FLIP
	ASI_AUTO_MAX_GAIN
	ASI_AUTO_MAX_EXP
	ASI_AUTO_TARGET_BRIGHTNESS
	ASI_HARDWARE_BIN
	ASI_HIGH_SPEED_MODE
	ASI_COOLER_POWER_PERC
	ASI_TARGET_TEMP
	ASI_COOLER_ON
	ASI_MONO_BIN
	ASI_FAN_ON
	ASI_PATTERN_ADJUST
	ASI_ANTI_DEW_HEATER
	ASI_FAN_ADJUST
	ASI_PWRLED_BRIGNT
	ASI_USBHUB_RESET
	ASI_GPS_SUPPORT
	ASI_GPS_START_LINE
	ASI_GPS_END_LINE
	ASI_ROLLING_INTERVAL //microsecond
)

type AsiCameraInfo struct {
	Name                 string //the name of the camera, you can display this to the UI
	CameraID             int    //this is used to control everything of the camera in other functions.Start from 0.
	MaxHeight            int    //the max height of the camera
	MaxWidth             int    //the max width of the camera
	IsColorCam           bool
	BayerPattern         int
	SupportedBins        []int   //1 means bin1 which is supported by every camera, 2 means bin 2 etc.. 0 is the end of supported binning method
	SupportedVideoFormat []int   //this array will content with the support output format type.IMG_END is the end of supported video format
	PixelSize            float64 //the pixel size of the camera, unit is um. such like 5.6um
	MechanicalShutter    bool
	ST4Port              bool
	IsCoolerCam          bool
	IsUSB3Host           bool
	IsUSB3Camera         bool
	ElecPerADU           float64
	BitDepth             int
	IsTriggerCam         bool
}

type AsiCameraControl struct {
	Name             string //the name of the Control like Exposure, Gain etc..
	Description      string //description of this control
	MaxValue         int
	MinValue         int
	DefaultValue     int
	IsAutoSupported  bool //support auto set 1, don't support 0
	IsWritable       bool //some control like temperature can only be read by some cameras
	ASI_Control_Type int  //this is used to get value and set value of the control
}

type GoAsiCamera struct {
	CameraID          int //inside CameraInfo
	IsOpen            bool
	IsInit            bool
	NControls         int
	CaptureWidth      int
	CaptureHeight     int
	OffsetX           int
	OffsetY           int
	Binning           int
	ImgFormat         int
	ExposureTS        time.Time
	ExposureTemp      float64
	ExposureGamma     int
	ExposureWbR       int
	ExposureWbB       int
	ExposureCount     int
	SerDuration       time.Duration
	Exposure          time.Duration
	Gain              int
	Offset            int
	FbSize            int
	TempSetp          int
	TecEnable         int
	DHEnable          int
	HighSpeedMode     int
	BandwidthOverload int
	CameraInfo        AsiCameraInfo
	BaseDir           string
	SubDir            string
	FrameType         string
	BaseName          string
	BaseSeq           int
	DateFmt           string
	Telescope         string
	FocalLengthMM     float64
	Observer          string
}

type SerHeader struct {
	FileID             []byte // 14 Bytes [0:14] ASCII fixed "LUCAM-RECORDER"
	LuID               uint32 //  4 Bytes [14:18] fixed 0
	ColorID            uint32 //  4 Bytes [18:22] MONO:0 BAYER_RGGB:8 +others
	LittleEndian       uint32 //  4 Bytes [22:26] 1 (TRUE) for little-endian byte order in 16 bit image data
	ImageWidth         uint32 //  4 Bytes [26:30] asi.CaptureWidth
	ImageHeight        uint32 //  4 Bytes [30:34] asi.CaptureHeight
	PixelDepthPerPlane uint32 //  4 Bytes [34:38] 2*1 for Mono single plane 16bit data
	FrameCount         uint32 //  4 Bytes [38:42] update after capture
	Observer           []byte // 40 Bytes [42:82] ASCII
	Instrument         []byte // 40 Bytes [82:122] ASCII
	Telescope          []byte // 40 Bytes [122:162] ASCII
	DateTime           uint64 //  8 Bytes [162:170] //EncodeSerDate(ts_in time.Time)
	DateTime_UTC       uint64 //  8 Bytes [170:178] //EncodeSerDate(ts_in time.Time)
}

type SerTrailer struct {
}

func EncodeSerDate(ts_in time.Time) int64 {
	//SER nanoseconds at unix epoch
	//Each increment represents 100 nanoseconds of elapsed time since the beginning of January 1 of the year 1

	offset := int64(365.25 * 24 * 60 * 60) // 365.25 x 24 x 60 x 60 = 31557600
	//offset := int64(31557600)    // 365.25 x 24 x 60 x 60 = 31557600
	offset = offset * 1e7             // ser 100ns increments
	offset = offset * (1970 - 1)      // ser epoch jan 01 0001
	offset = offset - 6*60*60*1e7     // arbitrary amount to make it work
	offset = offset - 16*24*60*60*1e7 // arbitrary amount to make it work

	unix_ns := ts_in.UnixNano()

	offset = offset + (unix_ns / 100) //add time since unix epoch

	return offset
}

func DecodeSerDate(ser_in int64) time.Time {

	offset := int64(365.25 * 24 * 60 * 60) // 365.25 x 24 x 60 x 60 = 31557600
	//offset := int64(31557600)    // 365.25 x 24 x 60 x 60 = 31557600
	offset = offset * 1e7             // ser 100ns increments
	offset = offset * (1970 - 1)      // ser epoch jan 01 0001
	offset = offset + 6*60*60*1e7     // arbitrary amount to make it work
	offset = offset + 16*24*60*60*1e7 // arbitrary amount to make it work

	unix_ns := (ser_in - offset) * 100

	ts := time.Unix(0, unix_ns)

	return ts
}

func ArcSecPerPixel(PixelSize, FocalLength float64) float64 {
	return (206.3 * PixelSize) / FocalLength
}

func ASIGetNumOfConnectedCameras() int {
	return int(C.ASIGetNumOfConnectedCameras())
}

//ASICAMERA_API ASI_BOOL ASICameraCheck(int iVID, int iPID);
//func ASIGetNumOfConnectedCameras(int vid, pid) int {
//	return int(C.ASICameraCheck())
//}

func Set_C_ASI_BOOL(input C.ASI_BOOL) bool {
	var output = false
	if int(input) == 1 {
		output = true
	}
	return output
}

func (asi *GoAsiCamera) ASIGetCameraProperty() int {
	var s_asi_info C.struct__ASI_CAMERA_INFO
	cid := C.int(asi.CameraID)
	res := int(C.ASIGetCameraProperty(&s_asi_info, cid))

	var name []byte
	for _, j := range s_asi_info.Name {
		if j == 0 { // ASI Name is a fixed 64-byte NUL-padded C string
			break
		}
		name = append(name, byte(j))
	}

	asi.CameraInfo.Name = string(name)
	asi.CameraInfo.CameraID = int(s_asi_info.CameraID)
	asi.CameraInfo.MaxHeight = int(s_asi_info.MaxHeight)
	asi.CameraInfo.MaxWidth = int(s_asi_info.MaxWidth)
	asi.CameraInfo.IsColorCam = Set_C_ASI_BOOL(s_asi_info.IsColorCam)
	asi.CameraInfo.BayerPattern = int(s_asi_info.BayerPattern)

	for _, j := range s_asi_info.SupportedBins {
		if j != 0 {
			asi.CameraInfo.SupportedBins = append(asi.CameraInfo.SupportedBins, int(j))
		}
	}

	for _, j := range s_asi_info.SupportedVideoFormat {
		if j < 0 {
			break
		}
		asi.CameraInfo.SupportedVideoFormat = append(asi.CameraInfo.SupportedVideoFormat, int(j))
	}

	asi.CameraInfo.PixelSize = float64(s_asi_info.PixelSize)
	asi.CameraInfo.MechanicalShutter = Set_C_ASI_BOOL(s_asi_info.MechanicalShutter)
	asi.CameraInfo.ST4Port = Set_C_ASI_BOOL(s_asi_info.ST4Port)
	asi.CameraInfo.IsCoolerCam = Set_C_ASI_BOOL(s_asi_info.IsCoolerCam)
	asi.CameraInfo.IsUSB3Host = Set_C_ASI_BOOL(s_asi_info.IsUSB3Host)
	asi.CameraInfo.IsUSB3Camera = Set_C_ASI_BOOL(s_asi_info.IsUSB3Camera)
	asi.CameraInfo.ElecPerADU = float64(s_asi_info.ElecPerADU)
	asi.CameraInfo.BitDepth = int(s_asi_info.BitDepth)
	asi.CameraInfo.IsTriggerCam = Set_C_ASI_BOOL(s_asi_info.IsTriggerCam)

	return res
}

func (asi *GoAsiCamera) ASIGetCameraPropertyByID() int {
	//var cameraInfo AsiCameraInfo
	var s_asi_info C.struct__ASI_CAMERA_INFO
	cid := C.int(asi.CameraID)
	res := int(C.ASIGetCameraPropertyByID(cid, &s_asi_info))

	var name []byte
	for _, j := range s_asi_info.Name {
		if j == 0 { // ASI Name is a fixed 64-byte NUL-padded C string
			break
		}
		name = append(name, byte(j))
	}

	asi.CameraInfo.Name = string(name)
	asi.CameraInfo.CameraID = int(s_asi_info.CameraID)
	asi.CameraInfo.MaxHeight = int(s_asi_info.MaxHeight)
	asi.CameraInfo.MaxWidth = int(s_asi_info.MaxWidth)
	asi.CameraInfo.IsColorCam = Set_C_ASI_BOOL(s_asi_info.IsColorCam)
	asi.CameraInfo.BayerPattern = int(s_asi_info.BayerPattern)

	for _, j := range s_asi_info.SupportedBins {
		if j != 0 {
			asi.CameraInfo.SupportedBins = append(asi.CameraInfo.SupportedBins, int(j))
		}
	}

	for _, j := range s_asi_info.SupportedVideoFormat {
		if j < 0 {
			break
		}
		asi.CameraInfo.SupportedVideoFormat = append(asi.CameraInfo.SupportedVideoFormat, int(j))
	}

	asi.CameraInfo.PixelSize = float64(s_asi_info.PixelSize)
	asi.CameraInfo.MechanicalShutter = Set_C_ASI_BOOL(s_asi_info.MechanicalShutter)
	asi.CameraInfo.ST4Port = Set_C_ASI_BOOL(s_asi_info.ST4Port)
	asi.CameraInfo.IsCoolerCam = Set_C_ASI_BOOL(s_asi_info.IsCoolerCam)
	asi.CameraInfo.IsUSB3Host = Set_C_ASI_BOOL(s_asi_info.IsUSB3Host)
	asi.CameraInfo.IsUSB3Camera = Set_C_ASI_BOOL(s_asi_info.IsUSB3Camera)
	asi.CameraInfo.ElecPerADU = float64(s_asi_info.ElecPerADU)
	asi.CameraInfo.BitDepth = int(s_asi_info.BitDepth)
	asi.CameraInfo.IsTriggerCam = Set_C_ASI_BOOL(s_asi_info.IsTriggerCam)

	return res
}

func (asi *GoAsiCamera) HardResetAfterSer() {
	asi.ASICloseCamera()
	asi.ASIOpenCamera()
	asi.ASIInitCamera()
	asi.SensibleDefaults()
}

func (asi *GoAsiCamera) SensibleDefaults() int {
	//set up sensible capture defaults
	asi.CaptureWidth = asi.CameraInfo.MaxWidth
	asi.CaptureHeight = asi.CameraInfo.MaxHeight
	asi.OffsetX = 0
	asi.OffsetY = 0

	asi.Binning = asi.CameraInfo.SupportedBins[0]
	asi.ImgFormat = ASI_IMG_RAW16

	// Default to RAW16 for all cameras (color cams stay Bayer-mosaiced for
	// client-side debayer at full bit depth). For RGB24, call SetImgFormat.
	// ASISetROIFormat sets FbSize from the format.
	asi.ASISetROIFormat(asi.CaptureWidth, asi.CaptureHeight, asi.Binning, asi.ImgFormat)
	//rw, rh, rb, rt := asi.ASIGetROIFormat()
	//fmt.Printf("ROI SET: %vx%v bin:%v type:%v\n", rw, rh, rb, rt)

	asi.ASISetStartPos(asi.OffsetX, asi.OffsetY)
	//sx, sy := asi.ASIGetStartPos()
	//fmt.Printf("ROI START: %v, %v\n", sx, sy)

	asi.ASISetCameraMode(ASI_MODE_NORMAL)

	asi.SetExposure(500 * time.Millisecond)
	asi.SetOffset(50)
	asi.SetGain(100)

	asi.SetGamma(50)

	//white balance based on full moon capture
	//asi.SetWbR(52)
	//asi.SetWbB(95)

	asi.SetWbR(50)
	asi.SetWbB(50)

	asi.GetGamma()
	asi.GetWbR()
	asi.GetWbB()

	asi.SetTemp(10)
	asi.SetTECState(1)
	//asi.GetTECPower()
	asi.SetDHState(1)
	asi.GetTemp()

	asi.BaseDir = "./"
	asi.FrameType = "Light"
	asi.DateFmt = "20060201_150405"
	asi.ExposureCount = 0

	asi.SetHighSpeedMode(1)
	asi.SetBandwidthOverload(100)

	//asi.ASISetControlValue(ASI_OVERCLOCK, cv_in, 0)
	//asi.ASISetControlValue(ASI_AUTO_MAX_GAIN, cv_in, 0)
	//asi.ASISetControlValue(ASI_AUTO_MAX_EXP, cv_in, 0)
	//asi.ASISetControlValue(ASI_AUTO_TARGET_BRIGHTNESS, cv_in, 0)
	//asi.ASISetControlValue(ASI_HARDWARE_BIN, cv_in, 0)
	//asi.ASISetControlValue(ASI_MONO_BIN, cv_in, 0)
	//asi.ASISetControlValue(ASI_PATTERN_ADJUST, cv_in, 0)

	return 0
}

func (asi *GoAsiCamera) SetExposure(exp_in time.Duration) {
	asi.Exposure = exp_in
	//exp_ns := int(asi.Exposure)
	exp_us := int(asi.Exposure) / 1000.0
	//fmt.Printf("ASI_EXPOSURE %v %v %v \n", asi.Exposure, exp_ns, exp_us)
	asi.ASISetControlValue(ASI_EXPOSURE, exp_us, 0) //us
	_, v, _ := asi.ASIGetControlValue(ASI_EXPOSURE)

	if v != exp_us {
		fmt.Printf("ASI_EXPOSURE Set Error. expected %v but got %v\n", exp_us, v)
	}
}

func (asi *GoAsiCamera) SetGain(exp_in int) {
	asi.Gain = exp_in
	asi.ASISetControlValue(ASI_GAIN, asi.Gain, 0) //us
	_, v, _ := asi.ASIGetControlValue(ASI_GAIN)
	if v != asi.Gain {
		fmt.Printf("ASI_GAIN Set Error. expected %v but got %v\n", asi.Gain, v)
	}
}

func (asi *GoAsiCamera) SetOffset(exp_in int) {
	asi.Offset = exp_in
	asi.ASISetControlValue(ASI_OFFSET, asi.Offset, 0) //us
	_, v, _ := asi.ASIGetControlValue(ASI_OFFSET)
	if v != asi.Offset {
		fmt.Printf("ASI_OFFSET Set Error. expected %v but got %v\n", asi.Offset, v)
	}
}

func (asi *GoAsiCamera) SetGamma(v_in int) {
	asi.ExposureGamma = v_in
	asi.ASISetControlValue(ASI_GAMMA, asi.ExposureGamma, 0) //C
	_, v, _ := asi.ASIGetControlValue(ASI_GAMMA)
	if v != asi.ExposureGamma {
		fmt.Printf("ASI_GAMMA Set Error. expected %v but got %v\n", asi.ExposureGamma, v)
	}
}

func (asi *GoAsiCamera) GetGamma() int {
	_, gamma, _ := asi.ASIGetControlValue(ASI_GAMMA)
	asi.ExposureGamma = gamma
	return asi.ExposureGamma
}

func (asi *GoAsiCamera) SetWbR(v_in int) {
	asi.ExposureWbR = v_in
	asi.ASISetControlValue(ASI_WB_R, asi.ExposureWbR, 0) //C
	_, v, _ := asi.ASIGetControlValue(ASI_WB_R)
	if v != asi.ExposureWbR {
		fmt.Printf("ASI_WB_R Set Error. expected %v but got %v\n", asi.ExposureWbR, v)
	}
}

func (asi *GoAsiCamera) GetWbR() int {
	_, wbr, _ := asi.ASIGetControlValue(ASI_WB_R)
	asi.ExposureWbR = wbr
	return asi.ExposureWbR
}

func (asi *GoAsiCamera) SetWbB(v_in int) {
	asi.ExposureWbB = v_in
	asi.ASISetControlValue(ASI_WB_B, asi.ExposureWbB, 0) //C
	_, v, _ := asi.ASIGetControlValue(ASI_WB_B)
	if v != asi.ExposureWbB {
		fmt.Printf("ASI_WB_B Set Error. expected %v but got %v\n", asi.ExposureWbB, v)
	}
}

func (asi *GoAsiCamera) GetWbB() int {
	_, wbb, _ := asi.ASIGetControlValue(ASI_WB_B)
	asi.ExposureWbB = wbb
	return asi.ExposureWbB
}

func (asi *GoAsiCamera) SetTemp(temp_in int) {
	asi.TempSetp = temp_in
	asi.ASISetControlValue(ASI_TARGET_TEMP, asi.TempSetp, 0) //C
	_, v, _ := asi.ASIGetControlValue(ASI_TARGET_TEMP)
	if v != asi.TempSetp {
		fmt.Printf("ASI_TARGET_TEMP Set Error. expected %v but got %v\n", asi.TempSetp, v)
	}
}

func (asi *GoAsiCamera) GetTemp() float64 {
	_, v, _ := asi.ASIGetControlValue(ASI_TEMPERATURE)
	asi.ExposureTemp = float64(v) / float64(10.0)
	return asi.ExposureTemp
}

func (asi *GoAsiCamera) SetTECState(tec_in int) {
	asi.TecEnable = tec_in
	asi.ASISetControlValue(ASI_COOLER_ON, asi.TecEnable, 0) // 1 or 0
	_, v, _ := asi.ASIGetControlValue(ASI_COOLER_ON)
	if v != asi.TecEnable {
		fmt.Printf("ASI_COOLER_ON Set Error. expected %v but got %v\n", asi.TecEnable, v)
	}
}

func (asi *GoAsiCamera) GetTECPower() int {

	//asi.ASISetControlValue(ASI_COOLER_POWER_PERC, 100, 0) // probably does nothing.
	_, v, _ := asi.ASIGetControlValue(ASI_COOLER_POWER_PERC)
	//fmt.Printf("ASI_COOLER_POWER_PERC: %v, %v\n", v, a)

	return v
}

func (asi *GoAsiCamera) SetDHState(dh_in int) {
	asi.DHEnable = dh_in
	asi.ASISetControlValue(ASI_COOLER_ON, asi.DHEnable, 0) // 1 or 0
	_, v, _ := asi.ASIGetControlValue(ASI_COOLER_ON)
	if v != asi.DHEnable {
		fmt.Printf("ASI_ANTI_DEW_HEATER Set Error. expected %v but got %v\n", asi.DHEnable, v)
	}
}

func (asi *GoAsiCamera) SetHighSpeedMode(dh_in int) {
	asi.HighSpeedMode = dh_in
	asi.ASISetControlValue(ASI_HIGH_SPEED_MODE, asi.HighSpeedMode, 0) // 1 or 0
	_, v, _ := asi.ASIGetControlValue(ASI_HIGH_SPEED_MODE)
	if v != asi.HighSpeedMode {
		fmt.Printf("ASI_HIGH_SPEED_MODE Set Error. expected %v but got %v\n", asi.HighSpeedMode, v)
	}
}

func (asi *GoAsiCamera) SetBandwidthOverload(dh_in int) {
	asi.BandwidthOverload = dh_in
	asi.ASISetControlValue(ASI_BANDWIDTHOVERLOAD, asi.BandwidthOverload, 0) // 100-0
	_, v, _ := asi.ASIGetControlValue(ASI_BANDWIDTHOVERLOAD)
	if v != asi.BandwidthOverload {
		fmt.Printf("ASI_BANDWIDTHOVERLOAD Set Error. expected %v but got %v\n", asi.BandwidthOverload, v)
	}
}

func (asi *GoAsiCamera) ASIOpenCamera() int {
	cid := C.int(asi.CameraID)
	res := int(C.ASIOpenCamera(cid))
	if res == 0 {
		asi.IsOpen = true
	}
	return res
}

func (asi *GoAsiCamera) ASIInitCamera() int {
	cid := C.int(asi.CameraID)
	res := int(C.ASIInitCamera(cid))
	if res == 0 {
		asi.IsInit = true
	}
	return res
}

func (asi *GoAsiCamera) ASICloseCamera() int {
	cid := C.int(asi.CameraID)
	res := int(C.ASICloseCamera(cid))
	if res == 0 {
		asi.IsOpen = false
		asi.IsInit = false
	}
	return res
}

func (asi *GoAsiCamera) ASIGetNumOfControls() int {
	cid := C.int(asi.CameraID)
	var x C.int
	res := int(C.ASIGetNumOfControls(cid, &x))
	if res > 0 {
		fmt.Printf("%+v\n", ASI_Error_Code_Message(res))
	}
	asi.NControls = int(x)
	return res
}

func (asi *GoAsiCamera) ASIPrintControls() int {

	for i := 0; i < asi.NControls; i++ {
		_, out := asi.ASIGetControlCaps(i)
		//if out.IsWritable {
		fmt.Printf("Control %v [%v]:\t", i, out.ASI_Control_Type)
		_, v, a := asi.ASIGetControlValue(out.ASI_Control_Type)
		fmt.Printf(" %v\t", v)

		//fmt.Printf("%+v  ", out)
		fmt.Printf("%+v  ", out.Name)
		//fmt.Printf("%+v  ", out.IsWritable)
		fmt.Printf("(%v, %v, %v)", out.MinValue, out.DefaultValue, out.MaxValue)

		if a > 0 {
			fmt.Printf(" + auto enabled", a)
		}
		fmt.Printf("\n")

		//}
	}
	return 0
}

func (asi *GoAsiCamera) ASIGetControlCaps(ci_in int) (int, AsiCameraControl) {
	cid := C.int(asi.CameraID)
	ci := C.int(ci_in)
	var y C.struct__ASI_CONTROL_CAPS
	var out AsiCameraControl

	res := int(C.ASIGetControlCaps(cid, ci, &y))

	var name []byte
	for _, j := range y.Name {
		name = append(name, byte(j))
	}

	var description []byte
	for _, j := range y.Description {
		description = append(description, byte(j))
	}

	out.Name = string(name)
	out.Description = string(description)
	out.MaxValue = int(y.MaxValue)
	out.MinValue = int(y.MinValue)
	out.DefaultValue = int(y.DefaultValue)
	out.IsAutoSupported = Set_C_ASI_BOOL(y.IsAutoSupported)
	out.IsWritable = Set_C_ASI_BOOL(y.IsWritable)

	out.ASI_Control_Type = int(y.ControlType)

	if res > 0 {
		fmt.Printf("ASIGetControlCaps %+v\n", ASI_Error_Code_Message(res))
	}

	return res, out
}

func (asi *GoAsiCamera) ASIGetControlValue(t_in int) (int, int, int) {
	cid := C.int(asi.CameraID)
	t := C.int(t_in)
	var v C.long
	var a C.int

	res := int(C.ASIGetControlValue(cid, t, &v, &a))
	if res > 0 {
		fmt.Printf("ASIGetControlValue %+v\n", ASI_Error_Code_Message(res))
	}
	return res, int(v), int(a)
}

func (asi *GoAsiCamera) ASISetControlValue(ct_in, cv_in, a_in int) int {
	cid := C.int(asi.CameraID)
	ct := C.int(ct_in)
	cv := C.long(cv_in)
	a := C.int(a_in)

	res := int(C.ASISetControlValue(cid, ct, cv, a))
	if res > 0 {
		fmt.Printf("ASISetControlValue %+v\n", ASI_Error_Code_Message(res))
	}
	return res
}

func (asi *GoAsiCamera) ASISetROIFormat(w_in, h_in, b_in, t_in int) int {
	cid := C.int(asi.CameraID)
	w := C.int(w_in)
	h := C.int(h_in)
	b := C.int(b_in)
	t := C.int(t_in)

	res := int(C.ASISetROIFormat(cid, w, h, b, t))
	if res > 0 {
		fmt.Printf("ASISetROIFormat %+v\n", ASI_Error_Code_Message(res))
		return res
	}

	// ASISetROIFormat is the single authority for capture geometry/format and
	// the derived frame-buffer size. Anything that changes ROI or pixel format
	// must go through here so FbSize stays correct.
	asi.CaptureWidth = w_in
	asi.CaptureHeight = h_in
	asi.Binning = b_in
	asi.ImgFormat = t_in
	asi.FbSize = w_in * h_in * BytesPerPixel(t_in)
	return res
}

// BytesPerPixel is the SDK frame-buffer stride for an ASI image format.
// RGB24 is 8-bit B,G,R interleaved (note: BGR order, not RGB).
func BytesPerPixel(imgFormat int) int {
	switch imgFormat {
	case ASI_IMG_RGB24:
		return 3
	case ASI_IMG_RAW16:
		return 2
	case ASI_IMG_RAW8, ASI_IMG_Y8:
		return 1
	default:
		return 2
	}
}

// SetImgFormat selects the pixel format (ASI_IMG_RAW8/RGB24/RAW16/Y8) for
// subsequent exposures, re-applying the current ROI so FbSize is recomputed.
// RAW16 is the default; RGB24 is an opt-in (8-bit BGR, planetary/preview).
func (asi *GoAsiCamera) SetImgFormat(format int) int {
	return asi.ASISetROIFormat(asi.CaptureWidth, asi.CaptureHeight, asi.Binning, format)
}

func (asi *GoAsiCamera) ASIGetROIFormat() (int, int, int, int) {
	var w C.int
	var h C.int
	var b C.int
	var t C.int

	//cid := C.int(cid_in)
	cid := C.int(asi.CameraID)
	res := int(C.ASIGetROIFormat(cid, &w, &h, &b, &t))
	if res > 0 {
		fmt.Printf("ASIGetROIFormat %+v\n", ASI_Error_Code_Message(res))
	}
	return int(w), int(h), int(b), int(t)
}

func (asi *GoAsiCamera) ASISetStartPos(x_in, y_in int) int {
	cid := C.int(asi.CameraID)
	x := C.int(x_in)
	y := C.int(y_in)
	res := int(C.ASISetStartPos(cid, x, y))
	if res > 0 {
		fmt.Printf("ASISetStartPos %+v\n", ASI_Error_Code_Message(res))
	}
	return int(res)
}

func (asi *GoAsiCamera) ASIGetStartPos() (int, int) {
	var x C.int
	var y C.int
	cid := C.int(asi.CameraID)
	res := int(C.ASIGetStartPos(cid, &x, &y))
	if res > 0 {
		fmt.Printf("ASIGetStartPos %+v\n", ASI_Error_Code_Message(res))
	}
	return int(x), int(y)
}

func (asi *GoAsiCamera) ASIGetDroppedFrames() (int, int) {
	cid := C.int(asi.CameraID)
	var dropped C.int
	res := int(C.ASIGetDroppedFrames(cid, &dropped))
	if res > 0 {
		fmt.Printf("ASIGetDroppedFrames %+v\n", ASI_Error_Code_Message(res))
	}
	return res, int(dropped)
}

func (asi *GoAsiCamera) ASIEnableDarkSubtract() int {
	//cid := C.int(asi.CameraID)
	//var dark_fn string
	//return int(C.ASIEnableDarkSubtract(cid, &dark_fn))
	return 0
}
func (asi *GoAsiCamera) ASIDisableDarkSubtract() int {
	cid := C.int(asi.CameraID)
	return int(C.ASIDisableDarkSubtract(cid))
}

func (asi *GoAsiCamera) ASIStartVideoCapture() int {
	cid := C.int(asi.CameraID)
	return int(C.ASIStartVideoCapture(cid))
}

func (asi *GoAsiCamera) ASIStopVideoCapture() int {
	cid := C.int(asi.CameraID)
	return int(C.ASIStopVideoCapture(cid))
}

// ASI_ERROR_CODE ASIGetVideoData(int iCameraID, unsigned char* pBuffer, long lBuffSize, int iWaitms);
func (asi *GoAsiCamera) ASIGetVideoData() {
	//cid := C.int(cid_in)
	//cid := C.int(asi.CameraID)
	//return int(C.ASIGetVideoData(cid))
}

func (asi *GoAsiCamera) ASIPulseGuideOn(dir_in int) int {
	cid := C.int(asi.CameraID)
	dir := C.int(dir_in)
	return int(C.ASIPulseGuideOn(cid, dir))
}

func (asi *GoAsiCamera) ASIPulseGuideOff(dir_in int) int {
	cid := C.int(asi.CameraID)
	dir := C.int(dir_in)
	return int(C.ASIPulseGuideOff(cid, dir))
}

func (asi *GoAsiCamera) UpdateExposureVars() int {

	asi.ExposureTS = time.Now()

	asi.GetTemp()

	asi.GetGamma()
	asi.GetWbR()
	asi.GetWbB()

	//fmt.Printf("ExposureVars temp:%+v, gamma:%+v, wbr:%+v, wbb:%+v\n",
	//   asi.ExposureTemp, asi.ExposureGamma, asi.ExposureWbR, asi.ExposureWbB)

	return 0
}

func (asi *GoAsiCamera) ASIStartExposure(isDark_in int) int {
	cid := C.int(asi.CameraID)
	isDark := C.int(isDark_in)

	res := int(C.ASIStartExposure(cid, isDark))
	if res > 0 {
		fmt.Printf("ASIStartExposure %+v\n", ASI_Error_Code_Message(res))
	}
	return res
}

func (asi *GoAsiCamera) ASIStopExposure() int {
	cid := C.int(asi.CameraID)
	res := int(C.ASIStopExposure(cid))
	if res > 0 {
		fmt.Printf("ASIStopExposure %+v\n", ASI_Error_Code_Message(res))
	}
	return res
}

func (asi *GoAsiCamera) ASIGetExpStatus() int {
	cid := C.int(asi.CameraID)
	var status C.ASI_EXPOSURE_STATUS
	res := int(C.ASIGetExpStatus(cid, &status))
	if res > 0 {
		fmt.Printf("ASIGetExpStatus %+v\n", int(res))
	}
	//fmt.Printf("ASI_EXPOSURE_STATUS %+v\n", int(status))

	return int(status)
}

// ASIGetExpStatusRC returns the exposure status together with the SDK error code
// (0 = success). Unlike ASIGetControlValue(ASI_TEMPERATURE) — which the SDK
// serves from a background-cached value — ASIGetExpStatus performs a live USB
// read, so a non-zero code reliably reports ASI_ERROR_CAMERA_REMOVED. It makes a
// liveness probe that does not re-enumerate the USB bus.
func (asi *GoAsiCamera) ASIGetExpStatusRC() (int, int) {
	cid := C.int(asi.CameraID)
	var status C.ASI_EXPOSURE_STATUS
	res := int(C.ASIGetExpStatus(cid, &status))
	return int(status), res
}
func (asi *GoAsiCamera) ReExposure() int {

	status := asi.ASIGetExpStatus()

	if status == 3 {
		//fmt.Printf("Exposure Status ASI_EXP_FAILED -- Re-Exposing (%v)\n", status)

		asi.UpdateExposureVars()

		asi.SetExposure(time.Duration(100 * time.Millisecond))

		status = asi.ASIStartExposure(0)
		//fmt.Printf("Re-start exposure...(%v)\n", status)

		time.Sleep(2 * asi.Exposure)
		status = asi.ASIGetExpStatus()
		//fmt.Printf("Exposure Status...(%v)\n", status)

		l := 0
		for l < 10 {
			//fmt.Printf("waiting for exposure success...\n")
			time.Sleep(2 * asi.Exposure)
			status = asi.ASIGetExpStatus()
			//fmt.Printf("Exposure Status...(%v)\n", status)
			if status == 2 {
				break
			}
			l++
		}

		if status == 3 {
			fmt.Printf("exposure failed again (%v)\n", status)
			return status
		}

		status = asi.ASIStopExposure()
		//fmt.Printf("ASIStopExposure() = %v\n", status)

	}

	status = asi.ASIGetExpStatus()
	//fmt.Printf("Final Status...(%v)\n", status)

	if status == 2 {
		//fmt.Printf("Exposure Status ASI_EXP_SUCCESS -- Re-Exposing (%v)\n", status)
		asi.ASIGetDataAfterExp()
	}

	status = asi.ASIGetExpStatus()
	//fmt.Printf("Final Status...(%v)\n", status)
	return status

}

func (asi *GoAsiCamera) SingleExposure() int {

	status := asi.ASIGetExpStatus()
	if status > 0 {
		//fmt.Printf("Exposure Status not Idle (probably just captured a SER) (%v)\n", status)
		status = asi.ReExposure()
	}
	if status > 0 {
		fmt.Printf("Exposure Status not Idle after ReExposure() (%v)\n", status)
		return status
	}

	asi.UpdateExposureVars()

	asi.ASIStartExposure(0)
	time.Sleep(asi.Exposure)
	l := 0
	for l < 10 {
		//fmt.Printf("waiting for exposure success...\n")
		time.Sleep(100 * time.Millisecond)
		status = asi.ASIGetExpStatus()
		if status == 2 {
			break
		}
		l++
	}

	if status > 2 {
		fmt.Printf("exposure failed\n")
		return status
	}

	asi.ASIStopExposure()
	asi.ExposureCount++

	return 0
}

func (asi *GoAsiCamera) VideoExposureWait() int {

	status := asi.ASIGetExpStatus()
	if status > 0 {
		fmt.Printf("Exposure Status not Idle %v\n", status)
		return -2
	}

	time.Sleep(asi.Exposure)

	for status < 2 {
		//fmt.Printf("expose sleep...\n")
		time.Sleep(1 * time.Millisecond)
		status = asi.ASIGetExpStatus()
	}

	if status > 2 {
		fmt.Printf("exposure failed\n")
		return -1
	}

	return 0
}

func (asi *GoAsiCamera) VideoExposureToSER() int {
	//asi.ExposureCount     int
	//asi.SerDuration       time.Duration

	// The SER header is hard-coded for 16-bit single-plane data; reject other
	// formats rather than write a mislabeled file.
	if asi.ImgFormat != ASI_IMG_RAW16 {
		fmt.Printf("VideoExposureToSER: only ASI_IMG_RAW16 supported (got %v)\n", asi.ImgFormat)
		return -1
	}

	count := asi.ExposureCount

	asi.UpdateExposureVars()

	fn := "unnamed.ser"

	if int(float64(asi.Exposure)/1e6) > 0 {
		fn = fmt.Sprintf("%v/%v/%v_%v_%v_%dms.ser", asi.BaseDir, asi.SubDir,
			asi.FrameType, asi.BaseName, asi.ExposureTS.Format(asi.DateFmt), int(float64(asi.Exposure)/1e6))
	} else {
		fn = fmt.Sprintf("%v/%v/%v_%v_%v_%dus.ser", asi.BaseDir, asi.SubDir,
			asi.FrameType, asi.BaseName, asi.ExposureTS.Format(asi.DateFmt), int(float64(asi.Exposure)/1e3))
	}

	f, err := os.Create(fn)
	if err != nil {
		log.Fatalf("could not create file: %+v", err)
	}
	defer f.Close()

	//fmt.Printf("%v\n", fn)

	headerBytes := make([]byte, 178)

	var observerBytes []byte
	var cameraBytes []byte
	var telescopeBytes []byte

	observerStr := []byte(asi.Observer)
	cameraStr := []byte(asi.CameraInfo.Name)
	telescopeStr := []byte(asi.Telescope)

	for i := 0; i < 40; i++ {
		observerStr = append(observerStr, 0)
		cameraStr = append(cameraStr, 0)
		telescopeStr = append(telescopeStr, 0)
	}

	for i := 0; i < 40; i++ {
		observerBytes = append(observerBytes, observerStr[i])
		cameraBytes = append(cameraBytes, cameraStr[i])
		telescopeBytes = append(telescopeBytes, telescopeStr[i])
	}

	copy(headerBytes[0:14], []byte("LUCAM-RECORDER"))
	binary.LittleEndian.PutUint32(headerBytes[14:18], uint32(0)) // int32 4 bytes
	binary.LittleEndian.PutUint32(headerBytes[18:22], uint32(8)) // MONO:0 BAYER_RGGB:8 +others
	binary.LittleEndian.PutUint32(headerBytes[22:26], uint32(0)) // 1 (TRUE) for little-endian 16 bit image data
	binary.LittleEndian.PutUint32(headerBytes[26:30], uint32(asi.CaptureWidth))
	binary.LittleEndian.PutUint32(headerBytes[30:34], uint32(asi.CaptureHeight))
	binary.LittleEndian.PutUint32(headerBytes[34:38], uint32(16))    // 2*1byte for Mono single plane 16bit data
	binary.LittleEndian.PutUint32(headerBytes[38:42], uint32(count)) // update after capture
	copy(headerBytes[42:82], observerBytes)
	copy(headerBytes[82:122], cameraBytes)
	copy(headerBytes[122:162], telescopeBytes)

	binary.LittleEndian.PutUint64(headerBytes[162:170], uint64(EncodeSerDate(asi.ExposureTS))) // set to zero if no trailer
	binary.LittleEndian.PutUint64(headerBytes[170:178], uint64(EncodeSerDate(asi.ExposureTS.UTC())))

	f.Write(headerBytes) // (n int, err error)
	f.Sync()

	//collect timestamps for trailer
	var trailerBuffer bytes.Buffer
	trailer := []byte{0, 0, 0, 0, 0, 0, 0, 0}

	frameCount := uint32(0)
	frameCountLast := uint32(0)

	//start video with initial delay
	asi.ASIStartVideoCapture()
	time.Sleep(250 * time.Millisecond)
	startTime := time.Now()
	thisTime := time.Now()

	durationLimited := false
	endTime := thisTime.Add(asi.SerDuration)

	if startTime.Before(endTime) {
		//fmt.Printf("SER Capture for Duration %v: %v to %v\n", asi.SerDuration, startTime, endTime)
		durationLimited = true
	}

	//buffer setup
	cid := C.int(asi.CameraID)
	lBuffSize := C.long(asi.FbSize)
	iWaitms := C.int((2 * asi.Exposure / 1.0e6)) // Exposure time in ms
	ptr := C.malloc((C.ulong)(asi.FbSize))
	defer C.free(unsafe.Pointer(ptr))

	//
	//
	//capture frame and update date and count

	//dropped count resets at image read
	dropped := int(0)
	droppedOffset := int(0)
	_, droppedOffset = asi.ASIGetDroppedFrames()
	trailerBuffer.Reset()

	for i := 0; i < count; i++ {

		_, dropped = asi.ASIGetDroppedFrames()
		res := int(C.ASIGetVideoData(cid, (*C.uchar)(ptr), lBuffSize, iWaitms))

		if res == 0 {
			//write out frame
			b := C.GoBytes(ptr, (C.int)(asi.FbSize))
			//fmt.Printf("%v\n", b[0:100])
			//fmt.Printf("min:%v max:%v\n", min(b), max(b))
			f.Write(b)
			f.Sync()

			//update trailer
			binary.LittleEndian.PutUint64(trailer, uint64(EncodeSerDate(time.Now().UTC())))
			trailerBuffer.Write(trailer)
			frameCount++

		} else {
			//fmt.Printf("retry\n")
			//retry frame
			i--
		}

		if frameCount%10 == 0 && frameCount > frameCountLast {
			thisTime = time.Now()
			secs := float64(thisTime.Sub(startTime)) / 1e9
			fmt.Printf("Frame Stats: Time: %0.2fs Captured: %v (%0.1f fps) Dropped: %v\n", secs,
				frameCount, float64(frameCount)/secs, dropped-droppedOffset)
			frameCountLast = frameCount

		}

		f.Sync()
		time.Sleep(1000 * time.Microsecond)

		// end loop
		if durationLimited && thisTime.After(endTime) {
			//fmt.Printf("SER Duration after end (%v): %v to %v\n", asi.SerDuration, thisTime, endTime)
			i = count
		}

	}

	//pad that should get overwritten by the trailer
	f.Write([]byte{'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a'})
	f.Sync()

	//stop video
	asi.ASIStopVideoCapture()

	//write trailer -- this is optional
	trailerStart := int64(178) + int64(frameCount)*int64(2*asi.CaptureWidth*asi.CaptureHeight)
	f.Seek(trailerStart, 0)

	f.Write(trailerBuffer.Bytes()) // (n int, err error)
	f.Sync()

	//seek back to correct the header if needed
	if uint32(count) != frameCount {
		//fmt.Printf("frameCount (%v) does not match requested count (%v)\n", frameCount, count)
		frameCountBytes := []byte{0, 0, 0, 0}
		binary.LittleEndian.PutUint32(frameCountBytes[0:], frameCount)
		f.Seek(38, 0)
		f.Write(frameCountBytes) // (n int, err error)
		f.Sync()
	}

	f.Sync()
	f.Close()

	return 0
}

func (asi *GoAsiCamera) ASIGetDataAfterExp() int {
	cid := C.int(asi.CameraID)
	lBuffSize := C.long(asi.FbSize)

	ptr := C.malloc((C.ulong)(asi.FbSize))
	defer C.free(unsafe.Pointer(ptr))

	res := int(C.ASIGetDataAfterExp(cid, (*C.uchar)(ptr), lBuffSize))

	//b := C.GoBytes(ptr, (C.int)(asi.FbSize))
	//fmt.Printf("%v\n", b[0:100])

	if res > 0 {
		fmt.Printf("ASIGetDataAfterExp %+v\n", ASI_Error_Code_Message(res))
	}

	return res

}

// ExposureFrame is a single raw readout in a transport-agnostic form, for
// consumers that need the pixels rather than a FITS file (e.g. Alpaca
// ImageBytes). Pixels are the SDK buffer verbatim: RAW16 is 16-bit unsigned
// little-endian with NO BZERO shift (unlike WriteExposureAsFIT, which subtracts
// 32768 for the signed-16 FITS convention). The caller owns ImageBytes
// header/element-type mapping.
//
// Format-specific notes for the consumer:
//   - RAW16 (mono or Bayer): rank 2, W*H little-endian uint16, one plane.
//   - RAW8/Y8:               rank 2, W*H bytes, one plane.
//   - RGB24:                 rank 3, W*H*3 bytes, 8-bit interleaved and in
//     BGR order — swap B<->R for ASCOM/clients (RGB).
type ExposureFrame struct {
	Width        int // pixels after binning (asi.CaptureWidth)
	Height       int // pixels after binning (asi.CaptureHeight)
	ImgFormat    int // ASI_IMG_RAW8 / RGB24 / RAW16 / Y8
	Binning      int
	BitDepth     int // sensor bit depth, e.g. 12 or 14 in a 16-bit container
	IsColor      bool
	BayerPattern int // ASI_BAYER_* ; meaningful only when IsColor and a raw format
	Pixels       []byte
}

// GetExposureBytes copies the just-finished exposure into an ExposureFrame.
// Returns the ASI error code (0 == success); on a non-zero code the frame is
// zero-valued. Call once ASIGetExpStatus reports success (== 2).
func (asi *GoAsiCamera) GetExposureBytes() (int, ExposureFrame) {
	cid := C.int(asi.CameraID)
	lBuffSize := C.long(asi.FbSize)

	ptr := C.malloc((C.ulong)(asi.FbSize))
	defer C.free(unsafe.Pointer(ptr))

	res := int(C.ASIGetDataAfterExp(cid, (*C.uchar)(ptr), lBuffSize))
	if res > 0 {
		fmt.Printf("GetExposureBytes %+v\n", ASI_Error_Code_Message(res))
		return res, ExposureFrame{}
	}

	frame := ExposureFrame{
		Width:        asi.CaptureWidth,
		Height:       asi.CaptureHeight,
		ImgFormat:    asi.ImgFormat,
		Binning:      asi.Binning,
		BitDepth:     asi.CameraInfo.BitDepth,
		IsColor:      asi.CameraInfo.IsColorCam,
		BayerPattern: asi.CameraInfo.BayerPattern,
		Pixels:       C.GoBytes(ptr, C.int(asi.FbSize)),
	}
	return res, frame
}

func (asi *GoAsiCamera) WriteExposureAsFIT() int {
	// The FITS writer assumes 16-bit single-plane data (BITPIX 16, BZERO 32768).
	// RGB24/RAW8/Y8 would be misread, so reject anything but RAW16.
	if asi.ImgFormat != ASI_IMG_RAW16 {
		fmt.Printf("WriteExposureAsFIT: only ASI_IMG_RAW16 supported (got %v)\n", asi.ImgFormat)
		return -1
	}

	cid := C.int(asi.CameraID)
	lBuffSize := C.long(asi.FbSize)

	ptr := C.malloc((C.ulong)(asi.FbSize))
	defer C.free(unsafe.Pointer(ptr))

	res := int(C.ASIGetDataAfterExp(cid, (*C.uchar)(ptr), lBuffSize))

	//fmt.Printf("fb: %v, %v, %v uint8s\n", asi.CaptureWidth, asi.CaptureHeight, 2)
	//fmt.Printf("expected buf size: %v\n", asi.CaptureWidth*asi.CaptureHeight*2)
	//fmt.Printf("lBuffSize: %v\n", asi.FbSize)

	b := C.GoBytes(ptr, (C.int)(asi.FbSize))
	//fmt.Printf("%v\n", b[0:50])

	//var pixel uint16
	data := make([]uint16, asi.FbSize/2)

	for j := 0; j < asi.FbSize/2; j++ {
		//data[j] = (binary.LittleEndian.Uint16(b[2*(j+0) : 2*(j+1)]))
		data[j] = (binary.LittleEndian.Uint16(b[2*j:])) - 32768
	}
	//fmt.Printf("%v\n", data[0:25])

	if res > 0 {
		fmt.Printf("ASIGetDataAfterExp %+v\n", ASI_Error_Code_Message(res))
		return -1
	}
	//
	//
	//
	//
	//
	fn := "unnamed.fits"
	if int(float64(asi.Exposure)/1e6) > 0 {
		fn = fmt.Sprintf("%v/%v/%v_%v_%v_%05d_%dms.fits", asi.BaseDir, asi.SubDir, asi.FrameType, asi.BaseName, asi.ExposureTS.Format(asi.DateFmt), asi.BaseSeq+asi.ExposureCount, int(float64(asi.Exposure)/1e6))
	} else {
		fn = fmt.Sprintf("%v/%v/%v_%v_%v_%05d_%dus.fits", asi.BaseDir, asi.SubDir, asi.FrameType, asi.BaseName, asi.ExposureTS.Format(asi.DateFmt), asi.BaseSeq+asi.ExposureCount, int(float64(asi.Exposure)/1e3))

	}

	//fmt.Printf("%+v\n", fn)
	f, err := os.Create(fn)
	if err != nil {
		log.Fatalf("could not create file: %+v", err)
	}
	defer f.Close()

	fits, err := fitsio.Create(f)
	if err != nil {
		log.Fatalf("could not create FITS file: %+v", err)
	}
	defer fits.Close()

	// create primary HDU image
	var (
		bitpix = 16
		axes   = []int{asi.CaptureWidth, asi.CaptureHeight}
	)
	img := fitsio.NewImage(bitpix, axes)
	defer img.Close()
	bayerpat := "NONE"
	if asi.CameraInfo.IsColorCam {
		bayerpat = "RGGB"
	}

	err = img.Header().Append(
		fitsio.Card{"EXTEND", "T", "FITS dataset may contain extensions"},
		fitsio.Card{"BZERO", 32768, "physical = BZERO + BSCALE*array_value"},
		fitsio.Card{"BSCALE", 1, "physical = BZERO + BSCALE*array_value"},
		fitsio.Card{"XBINNING", asi.Binning, "Binning factor in width"},
		fitsio.Card{"YBINNING", asi.Binning, "Binning factor in height"},
		fitsio.Card{"EXPOINUS", float64(asi.Exposure) / 1e3, "Exposure time in us"},
		fitsio.Card{"GAIN", asi.Gain, "The ratio of output / input"},
		fitsio.Card{"OFFSET", asi.Offset, "Brightness(offset) of image"},
		fitsio.Card{"GAMMA", asi.ExposureGamma, "Gamma"},
		fitsio.Card{"WB_RED", asi.ExposureWbR, "White Balance(Red)"},
		fitsio.Card{"WB_BLUE", asi.ExposureWbB, "White Balance(Blue)"},
		fitsio.Card{"CBLACK", 0, "Initial display black level in ADUs"},
		fitsio.Card{"CWHITE", 65535, "Initial display white level in ADUs"},
		fitsio.Card{"PEDISTAL", 0, "Correction to add for zero-based ADU"},
		fitsio.Card{"SWCREATE", "GoAsi", "Name of software that created the image"},
		fitsio.Card{"SWOWNER", asi.Observer, "Person capturing this image"},
		fitsio.Card{"SCOPE", asi.Telescope, "Telescope Model"},
		fitsio.Card{"FOCALLMM", asi.FocalLengthMM, "Effective Focal Length"},
		fitsio.Card{"DATE-OBS", asi.ExposureTS.UTC().Format("2006-01-02T15:04:05Z"), "UTC start date of observation"},
		fitsio.Card{"BAYERPAT", bayerpat, "Debayer pattern,such as RGGB,BGGR,GRBG,GBRG"},
		fitsio.Card{"COLORTYP", "RAW16", "Color space, such as RAW8,RAW16,RGB24"},
		fitsio.Card{"INPUTFMT", "FITS", "Format of file from which image was read"},
		fitsio.Card{"SENSOR", asi.CameraInfo.Name, "Camera Model "},
		fitsio.Card{"XPIXSZ", asi.CameraInfo.PixelSize, "Pixel Width in um"},
		fitsio.Card{"YPIXSZ", asi.CameraInfo.PixelSize, "Pixel Height in um"},
		fitsio.Card{"EPERADU", asi.CameraInfo.ElecPerADU, "Electrons per ADU"},
		fitsio.Card{"CMOSBIT", asi.CameraInfo.BitDepth, "Sensor BitDepth"},
		fitsio.Card{"EXPTIME", float64(asi.Exposure) / 1e9, "Total Exposure Time (s)"},
		fitsio.Card{"EXPOSURE", float64(asi.Exposure) / 1e9, "Exposure time in seconds"},
		fitsio.Card{"CCD-TEMP", asi.ExposureTemp, "CMOS sensor temperature in C"},
		fitsio.Card{"ASECPIX", ArcSecPerPixel(asi.CameraInfo.PixelSize, asi.FocalLengthMM), "ARC SEC per Pixel"},
	)
	if err != nil {
		log.Fatalf("could append cards: %+v", err)
	}

	err = img.Write(data)
	if err != nil {
		log.Fatalf("could not write data to image: %+v", err)
	}

	err = fits.Write(img)
	if err != nil {
		log.Fatalf("could not write image to FITS file: %+v", err)
	}

	err = fits.Close()
	if err != nil {
		log.Fatalf("could not close FITS file: %+v", err)
	}

	err = f.Close()
	if err != nil {
		log.Fatalf("could not close file: %+v", err)
	}

	return res

}

func (asi *GoAsiCamera) ASIGetID() {
	cid := C.int(asi.CameraID)
	var asi_id C.ASI_ID
	res := int(C.ASIGetID(cid, &asi_id))
	if res > 0 {
		fmt.Printf("%+v\n", ASI_Error_Code_Message(res))
	}
	fmt.Printf("asi_id: %+v\n", asi_id)
	//for _, j := range ver {
	//	res = append(res, byte(j))
	//}
	//return string(ver)
}

//ASI_ERROR_CODE ASISetID(int iCameraID, ASI_ID ID);

func (asi *GoAsiCamera) ASIGetGainOffset() (int, int, int, int, int) {
	cid := C.int(asi.CameraID)
	var pOffset_HighestDR C.int
	var pOffset_UnityGain C.int
	var pGain_LowestRN C.int
	var pOffset_LowestRN C.int

	res := int(C.ASIGetGainOffset(cid, &pOffset_HighestDR, &pOffset_UnityGain, &pGain_LowestRN, &pOffset_LowestRN))
	if res > 0 {
		fmt.Printf("ASIGetGainOffset %+v\n", ASI_Error_Code_Message(res))
	}
	return int(res), int(pOffset_HighestDR), int(pOffset_UnityGain), int(pGain_LowestRN), int(pOffset_LowestRN)
}

func (asi *GoAsiCamera) ASIGetLMHGainOffset() (int, int, int, int, int) {
	cid := C.int(asi.CameraID)
	var pLGain C.int
	var pMGain C.int
	var pHGain C.int
	var pHOffset C.int

	res := int(C.ASIGetLMHGainOffset(cid, &pLGain, &pMGain, &pHGain, &pHOffset))
	if res > 0 {
		fmt.Printf("ASIGetLMHGainOffset %+v\n", ASI_Error_Code_Message(res))
	}
	return int(res), int(pLGain), int(pMGain), int(pHGain), int(pHOffset)
}

func ASIGetSDKVersion() {
	//var res []byte
	ver := C.ASIGetSDKVersion()
	fmt.Printf("ver: %+v\n", ver)
	//for _, j := range ver {
	//	res = append(res, byte(j))
	//}
	//return string(ver)
}

func (asi *GoAsiCamera) ASIGetCameraSupportMode() []int {
	cid := C.int(asi.CameraID)
	var pSupportedMode C.struct__ASI_SUPPORTED_MODE
	var mode []int

	res := int(C.ASIGetCameraSupportMode(cid, &pSupportedMode))
	if res > 0 {
		fmt.Printf("%+v\n", ASI_Error_Code_Message(res))
	}

	for _, j := range pSupportedMode.SupportedCameraMode {
		if j < 0 {
			break
		}
		mode = append(mode, int(j))
	}

	return mode

}

func (asi *GoAsiCamera) ASIGetCameraMode() int {
	cid := C.int(asi.CameraID)
	var mode C.ASI_CAMERA_MODE
	res := int(C.ASIGetCameraMode(cid, &mode))
	if res > 0 {
		fmt.Printf("%+v\n", ASI_Error_Code_Message(res))
	}
	return int(mode)
}

func (asi *GoAsiCamera) ASISetCameraMode(mode_in int) int {
	cid := C.int(asi.CameraID)
	mode := C.ASI_CAMERA_MODE(mode_in)
	res := int(C.ASISetCameraMode(cid, mode))
	if res > 0 {
		fmt.Printf("%+v\n", ASI_Error_Code_Message(res))
	}
	return res
}

func (asi *GoAsiCamera) ASISendSoftTrigger(trigger_in bool) int {
	cid := C.int(asi.CameraID)
	bStart := C.int(0)
	if trigger_in {
		bStart = 1
	}
	return int(C.ASISendSoftTrigger(cid, bStart))
}

func (asi *GoAsiCamera) ASIGetSerialNumber() string {
	cid := C.int(asi.CameraID)
	var s_asi_sn C.struct__ASI_ID
	res := int(C.ASIGetSerialNumber(cid, &s_asi_sn))
	if res > 0 {
		fmt.Printf("%+v\n", ASI_Error_Code_Message(res))
	}
	var sn []byte
	for _, j := range s_asi_sn.id {
		sn = append(sn, byte(j))
	}

	return string(sn)

}

func (asi *GoAsiCamera) ASISetTriggerOutputIOConf(pin_in, state_in, delay_in, dur_in int) int {
	cid := C.int(asi.CameraID)
	pin := C.ASI_TRIG_OUTPUT_PIN(pin_in)
	state := C.int(state_in)
	delay := C.long(delay_in)
	dur := C.long(dur_in)

	return int(C.ASISetTriggerOutputIOConf(cid, pin, state, delay, dur))
}

func (asi *GoAsiCamera) ASIGetTriggerOutputIOConf(pin_in, state_in, delay_in, dur_in int) int {
	cid := C.int(asi.CameraID)
	pin := C.ASI_TRIG_OUTPUT_PIN(pin_in)
	var state C.int
	var delay C.long
	var dur C.long

	return int(C.ASIGetTriggerOutputIOConf(cid, pin, &state, &delay, &dur))
}

func (asi *GoAsiCamera) ShowCaptureInfo() {
	fmt.Printf("CameraID:\t %v\n", asi.CameraID)              //int //inside CameraInfo
	fmt.Printf("IsOpen: \t %v\n", asi.IsOpen)                 //bool
	fmt.Printf("IsInit: \t %v\n", asi.IsInit)                 //bool
	fmt.Printf("NControls: \t %v\n", asi.NControls)           //int
	fmt.Printf("CaptureWidth: \t %v\n", asi.CaptureWidth)     //int
	fmt.Printf("CaptureHeight: \t %v\n", asi.CaptureHeight)   //int
	fmt.Printf("OffsetX: \t %v\n", asi.OffsetX)               //int
	fmt.Printf("OffsetY: \t %v\n", asi.OffsetY)               //int
	fmt.Printf("Binning: \t %v\n", asi.Binning)               //int
	fmt.Printf("ImgFormat: \t %v\n", asi.ImgFormat)           //int
	fmt.Printf("ExposureTS: \t %v\n", asi.ExposureTS)         //time.Time
	fmt.Printf("ExposureTemp: \t %v\n", asi.ExposureTemp)     //float64
	fmt.Printf("ExposureGamma: \t %v\n", asi.ExposureGamma)   //int
	fmt.Printf("ExposureWbR: \t %v\n", asi.ExposureWbR)       //int
	fmt.Printf("ExposureWbB: \t %v\n", asi.ExposureWbB)       //int
	fmt.Printf("ExposureCount: \t %v\n", asi.ExposureCount)   //int
	fmt.Printf("Exposure: \t %v\n", asi.Exposure)             //time.Duration
	fmt.Printf("SerDuration: \t %v\n", asi.SerDuration)       //time.Duration
	fmt.Printf("Gain:    \t %v\n", asi.Gain)                  //int
	fmt.Printf("Offset: \t %v\n", asi.Offset)                 //int
	fmt.Printf("FbSize: \t %v\n", asi.FbSize)                 //int
	fmt.Printf("TempSetp: \t %v\n", asi.TempSetp)             //int
	fmt.Printf("TecEnable: \t %v\n", asi.TecEnable)           //int
	fmt.Printf("DHEnable: \t %v\n", asi.DHEnable)             //int
	fmt.Printf("HighSpeedMode: \t %v\n", asi.HighSpeedMode)   //int
	fmt.Printf("BW Overload: \t %v\n", asi.BandwidthOverload) //int
	fmt.Printf("CameraInfo: \t %v\n", asi.CameraInfo)         //AsiCameraInfo
	fmt.Printf("BaseDir: \t %v\n", asi.BaseDir)               //string
	fmt.Printf("SubDir: \t %v\n", asi.SubDir)                 //string
	fmt.Printf("FrameType: \t %v\n", asi.FrameType)           //string
	fmt.Printf("BaseName: \t %v\n", asi.BaseName)             //string
	fmt.Printf("BaseSeq: \t %v\n", asi.BaseSeq)               //int
	fmt.Printf("DateFmt: \t %v\n", asi.DateFmt)               //string
	fmt.Printf("Telescope: \t %v\n", asi.Telescope)           //string
	fmt.Printf("FocalLengthMM: \t %v\n", asi.FocalLengthMM)   //float64
	fmt.Printf("Observer: \t %v\n", asi.Observer)             //string
}
