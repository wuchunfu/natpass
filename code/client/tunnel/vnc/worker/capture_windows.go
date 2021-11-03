package worker

import (
	"errors"
	"natpass/code/client/tunnel/vnc/define"
	"natpass/code/client/tunnel/vnc/vncnetwork"
	"syscall"
	"unsafe"

	"github.com/lwch/logging"
)

func (worker *Worker) runCapture() vncnetwork.ImageData {
	err := worker.capture()
	if err != nil {
		return vncnetwork.ImageData{
			Ok:  false,
			Msg: err.Error(),
		}
	}
	data := make([]byte, worker.info.width*worker.info.height*worker.info.bits/8)
	for i := 0; i < len(data); i++ {
		data[i] = *(*uint8)(unsafe.Pointer(worker.buffer + uintptr(i)))
	}
	// BGR => RGB
	for i := 0; i < len(data); i += (worker.info.bits / 8) {
		data[i], data[i+2] = data[i+2], data[i]
	}
	return vncnetwork.ImageData{
		Ok:     true,
		Bits:   uint32(worker.info.bits),
		Width:  uint32(worker.info.width),
		Height: uint32(worker.info.height),
		Data:   data,
	}
}

func (worker *Worker) capture() error {
	detach, err := attachDesktop()
	if err != nil {
		return errors.New("attach desktop: " + err.Error())
	}
	defer detach()
	info := worker.info
	err = worker.updateInfo()
	if err != nil {
		return errors.New("update info: " + err.Error())
	}
	if info.bits != worker.info.bits ||
		info.width != worker.info.width ||
		info.height != worker.info.height {
		err = worker.updateBuffer()
		if err != nil {
			return errors.New("update buffer: " + err.Error())
		}
	}
	logging.Info("width=%d, height=%d, bits=%d", info.width, info.height, info.bits)
	memDC, _, err := syscall.Syscall(define.FuncCreateCompatibleDC, 1, worker.hdc, 0, 0)
	if memDC == 0 {
		return errors.New("create dc: " + err.Error())
	}
	defer syscall.Syscall(define.FuncDeleteDC, 1, memDC, 0, 0)
	bitmap, _, err := syscall.Syscall(define.FuncCreateCompatibleBitmap, 3, worker.hdc,
		uintptr(worker.info.width), uintptr(worker.info.height))
	if bitmap == 0 {
		return errors.New("create bitmap: " + err.Error())
	}
	defer syscall.Syscall(define.FuncDeleteObject, 1, bitmap, 0, 0)
	oldDC, _, err := syscall.Syscall(define.FuncSelectObject, 2, memDC, bitmap, 0)
	if oldDC == 0 {
		return errors.New("select object: " + err.Error())
	}
	defer syscall.Syscall(define.FuncSelectObject, 2, memDC, oldDC, 0)
	ok, _, err := syscall.Syscall9(define.FuncBitBlt, 9, memDC, 0, 0,
		uintptr(worker.info.width), uintptr(worker.info.height), worker.hdc, 0, 0, define.SRCCOPY)
	if ok == 0 {
		return errors.New("bitblt: " + err.Error())
	}
	defer worker.copyImageData(bitmap)
	if !worker.showCursor {
		return nil
	}
	var curInfo define.CURSORINFO
	curInfo.CbSize = define.DWORD(unsafe.Sizeof(curInfo))
	ok, _, err = syscall.Syscall(define.FuncGetCursorInfo, 1, uintptr(unsafe.Pointer(&curInfo)), 0, 0)
	if ok == 0 {
		logging.Error("get cursor info: %v", err)
		return nil
	}
	if curInfo.Flags == define.CURSOR_SHOWING {
		var info define.ICONINFO
		ok, _, err = syscall.Syscall(define.FuncGetIconInfo, 2, uintptr(curInfo.HCursor), uintptr(unsafe.Pointer(&info)), 0)
		if ok == 0 {
			logging.Error("get icon info: %v", err)
			return nil
		}
		x := curInfo.PTScreenPos.X - define.LONG(info.XHotspot)
		y := curInfo.PTScreenPos.Y - define.LONG(info.YHotspot)
		ok, _, err = syscall.Syscall6(define.FuncDrawIcon, 4, memDC, uintptr(x), uintptr(y), uintptr(curInfo.HCursor), 0, 0)
		if ok == 0 {
			logging.Error("draw icon: %v", err)
			return nil
		}
	}
	return nil
}

type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

func (worker *Worker) copyImageData(bitmap uintptr) {
	var hdr BITMAPINFOHEADER
	hdr.BiSize = uint32(unsafe.Sizeof(hdr))
	hdr.BiPlanes = 1
	hdr.BiBitCount = uint16(worker.info.bits)
	hdr.BiWidth = int32(worker.info.width)
	hdr.BiHeight = int32(-worker.info.height)
	hdr.BiCompression = define.BI_RGB
	hdr.BiSizeImage = 0
	lines, _, err := syscall.Syscall9(define.FuncGetDIBits, 7, worker.hdc, bitmap, 0, uintptr(worker.info.height),
		worker.buffer, uintptr(unsafe.Pointer(&hdr)), define.DIB_RGB_COLORS, 0, 0)
	if lines == 0 {
		logging.Error("get bits: %v", err)
	}
}