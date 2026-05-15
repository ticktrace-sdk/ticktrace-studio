//go:build linux

package usbx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Linux usbfs ioctl numbers, computed for 64-bit (8-byte pointer) builds.
// 32-bit Linux is not a supported v1 target — bulk/control structs change
// size with pointer width, so the ioctl numbers would differ.
const (
	ioctlUsbdevfsControl          = 0xC0185500 // _IOWR('U', 0, struct usbdevfs_ctrltransfer) size=24
	ioctlUsbdevfsBulk             = 0xC0185502 // _IOWR('U', 2, struct usbdevfs_bulktransfer) size=24
	ioctlUsbdevfsClaimInterface   = 0x8004550F // _IOR('U', 15, unsigned int)
	ioctlUsbdevfsReleaseInterface = 0x80045510 // _IOR('U', 16, unsigned int)
	ioctlUsbdevfsDisconnect       = 0x00005516 // _IO('U', 22) — bound to a USBDEVFS_IOCTL request
	ioctlUsbdevfsIoctl            = 0xC0105512 // _IOWR('U', 18, struct usbdevfs_ioctl) size=16
)

// usbdevfsCtrlTransfer mirrors struct usbdevfs_ctrltransfer (sizeof = 24 on
// 64-bit Linux).
type usbdevfsCtrlTransfer struct {
	BmRequestType uint8
	BRequest      uint8
	WValue        uint16
	WIndex        uint16
	WLength       uint16
	Timeout       uint32 // ms
	_padding      uint32 // align Data to 8 on 64-bit
	Data          uintptr
}

// usbdevfsBulkTransfer mirrors struct usbdevfs_bulktransfer (sizeof = 24).
type usbdevfsBulkTransfer struct {
	Endpoint uint32
	Length   uint32
	Timeout  uint32 // ms
	_padding uint32
	Data     uintptr
}

// usbdevfsIoctl mirrors struct usbdevfs_ioctl (sizeof = 16).
type usbdevfsIoctl struct {
	Ifno      int32
	IoctlCode int32
	Data      uintptr
}

type linuxDevice struct {
	fd       int
	info     DeviceInfo
	ifaceNum uint8
	inEP     uint8 // 0x80-set, e.g. 0x83
	outEP    uint8 // e.g. 0x03
}

// Open finds a Raspberry Pi RP2350 in BOOTSEL mode, claims its PICOBOOT
// interface, and returns a Device.
func Open(opts OpenOptions) (Device, error) {
	cands, err := Enumerate()
	if err != nil {
		return nil, err
	}
	if opts.SerialFilter != "" {
		filtered := cands[:0]
		for _, c := range cands {
			if strings.Contains(c.Info.Serial, opts.SerialFilter) {
				filtered = append(filtered, c)
			}
		}
		cands = filtered
	}
	if len(cands) == 0 {
		return nil, ErrNoDevice
	}
	if len(cands) > 1 {
		return nil, ErrMultipleDevices
	}
	return openCandidate(cands[0])
}

// Candidate is an enumerated BOOTSEL device, returned from Enumerate. Callers
// generally don't construct these directly.
type Candidate struct {
	Info     DeviceInfo
	devNode  string
	ifaceNum uint8
	inEP     uint8
	outEP    uint8
}

// Enumerate walks /sys/bus/usb/devices for RP2350 BOOTSEL devices that expose
// a PICOBOOT interface (vendor class, subclass 0, protocol 1).
func Enumerate() ([]Candidate, error) {
	const root = "/sys/bus/usb/devices"
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("usbx: read %s: %w", root, err)
	}

	var out []Candidate
	for _, e := range entries {
		// USB device dirs are like "3-4" — they don't contain ':'. Interface
		// dirs are "3-4:1.1". Skip the latter at the top level; we'll find
		// them under their parent device dir.
		if strings.Contains(e.Name(), ":") {
			continue
		}
		devDir := filepath.Join(root, e.Name())
		vid, ok := readHex16(filepath.Join(devDir, "idVendor"))
		if !ok || vid != VendorRaspberryPi {
			continue
		}
		pid, ok := readHex16(filepath.Join(devDir, "idProduct"))
		if !ok || pid != ProductRP2350Bootsel {
			continue
		}
		c, err := buildCandidate(devDir, vid, pid)
		if err != nil {
			// One interface lookup failing doesn't disqualify the others.
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func buildCandidate(devDir string, vid, pid uint16) (Candidate, error) {
	busnum, _ := readDec(filepath.Join(devDir, "busnum"))
	devnum, _ := readDec(filepath.Join(devDir, "devnum"))
	serial := readString(filepath.Join(devDir, "serial"))

	devNode := fmt.Sprintf("/dev/bus/usb/%03d/%03d", busnum, devnum)
	ifaceNum, inEP, outEP, err := findPicobootInterface(devDir)
	if err != nil {
		return Candidate{}, err
	}
	return Candidate{
		Info: DeviceInfo{
			Vendor:  vid,
			Product: pid,
			Serial:  serial,
			BusAddr: devNode,
		},
		devNode:  devNode,
		ifaceNum: ifaceNum,
		inEP:     inEP,
		outEP:    outEP,
	}, nil
}

func findPicobootInterface(devDir string) (uint8, uint8, uint8, error) {
	entries, err := os.ReadDir(devDir)
	if err != nil {
		return 0, 0, 0, err
	}
	devBase := filepath.Base(devDir)
	prefix := devBase + ":"
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		ifaceDir := filepath.Join(devDir, e.Name())
		cls, ok := readHex8(filepath.Join(ifaceDir, "bInterfaceClass"))
		if !ok || cls != PicobootClass {
			continue
		}
		// Real RP2350 BOOTSEL devices ship with bSubClass=0, bProtocol=0
		// despite the SDK header's RESET_INTERFACE_PROTOCOL=1 (that constant
		// is for the *app-level* stdio reset interface, not BOOTSEL itself).
		// Picotool also only filters on class==0xff; we match that.
		ifaceNum, _ := readHex8(filepath.Join(ifaceDir, "bInterfaceNumber"))
		inEP, outEP, err := findBulkEndpoints(ifaceDir)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("interface %d: %w", ifaceNum, err)
		}
		return ifaceNum, inEP, outEP, nil
	}
	return 0, 0, 0, errors.New("no PICOBOOT interface found")
}

func findBulkEndpoints(ifaceDir string) (uint8, uint8, error) {
	entries, err := os.ReadDir(ifaceDir)
	if err != nil {
		return 0, 0, err
	}
	var in, out uint8
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "ep_") {
			continue
		}
		epDir := filepath.Join(ifaceDir, e.Name())
		attrs, _ := readHex8(filepath.Join(epDir, "bmAttributes"))
		if attrs&0x03 != 0x02 { // 0x02 = bulk
			continue
		}
		addr, ok := readHex8(filepath.Join(epDir, "bEndpointAddress"))
		if !ok {
			continue
		}
		if addr&0x80 != 0 {
			in = addr
		} else {
			out = addr
		}
	}
	if in == 0 || out == 0 {
		return 0, 0, errors.New("PICOBOOT interface missing bulk IN/OUT endpoint pair")
	}
	return in, out, nil
}

func openCandidate(c Candidate) (Device, error) {
	fd, err := syscall.Open(c.devNode, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		// EACCES is the udev-rules-missing case; surface a clear message.
		if errors.Is(err, fs.ErrPermission) {
			return nil, fmt.Errorf("usbx: open %s: permission denied (run `studio doctor` or `sudo` once)", c.devNode)
		}
		return nil, fmt.Errorf("usbx: open %s: %w", c.devNode, err)
	}

	if err := claimInterface(fd, c.ifaceNum); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("usbx: claim interface %d on %s: %w", c.ifaceNum, c.devNode, err)
	}

	return &linuxDevice{
		fd:       fd,
		info:     c.Info,
		ifaceNum: c.ifaceNum,
		inEP:     c.inEP,
		outEP:    c.outEP,
	}, nil
}

// claimInterface detaches any kernel driver and claims the interface for our
// userspace use. PICOBOOT shouldn't normally have a driver bound but the
// disconnect step is cheap insurance against stuck state.
func claimInterface(fd int, ifaceNum uint8) error {
	// First try USBDEVFS_DISCONNECT via USBDEVFS_IOCTL wrapper. If no driver
	// is attached the kernel returns ENODATA; ignore.
	wrapper := usbdevfsIoctl{
		Ifno:      int32(ifaceNum),
		IoctlCode: ioctlUsbdevfsDisconnect,
		Data:      0,
	}
	_, _, _ = syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(ioctlUsbdevfsIoctl),
		uintptr(unsafe.Pointer(&wrapper)),
	)

	iface := uint32(ifaceNum)
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(ioctlUsbdevfsClaimInterface),
		uintptr(unsafe.Pointer(&iface)),
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func (d *linuxDevice) Info() DeviceInfo { return d.info }

func (d *linuxDevice) Control(setup ControlSetup, data []byte, timeout time.Duration) (int, error) {
	wLength := uint16(len(data))
	var dataPtr uintptr
	if wLength > 0 {
		dataPtr = uintptr(unsafe.Pointer(&data[0]))
	}
	xfer := usbdevfsCtrlTransfer{
		BmRequestType: setup.BmRequestType,
		BRequest:      setup.BRequest,
		WValue:        setup.WValue,
		WIndex:        uint16(d.ifaceNum),
		WLength:       wLength,
		Timeout:       toMs(timeout),
		Data:          dataPtr,
	}
	r1, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(d.fd),
		uintptr(ioctlUsbdevfsControl),
		uintptr(unsafe.Pointer(&xfer)),
	)
	if errno != 0 {
		return 0, xlateErrno(errno)
	}
	return int(r1), nil
}

func (d *linuxDevice) BulkOut(data []byte, timeout time.Duration) (int, error) {
	return d.bulk(d.outEP, data, timeout)
}

func (d *linuxDevice) BulkIn(buf []byte, timeout time.Duration) (int, error) {
	return d.bulk(d.inEP, buf, timeout)
}

func (d *linuxDevice) bulk(ep uint8, buf []byte, timeout time.Duration) (int, error) {
	var dataPtr uintptr
	if len(buf) > 0 {
		dataPtr = uintptr(unsafe.Pointer(&buf[0]))
	}
	xfer := usbdevfsBulkTransfer{
		Endpoint: uint32(ep),
		Length:   uint32(len(buf)),
		Timeout:  toMs(timeout),
		Data:     dataPtr,
	}
	r1, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(d.fd),
		uintptr(ioctlUsbdevfsBulk),
		uintptr(unsafe.Pointer(&xfer)),
	)
	if errno != 0 {
		return 0, xlateErrno(errno)
	}
	return int(r1), nil
}

func (d *linuxDevice) Close() error {
	if d.fd < 0 {
		return nil
	}
	iface := uint32(d.ifaceNum)
	_, _, _ = syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(d.fd),
		uintptr(ioctlUsbdevfsReleaseInterface),
		uintptr(unsafe.Pointer(&iface)),
	)
	err := syscall.Close(d.fd)
	d.fd = -1
	return err
}

func toMs(d time.Duration) uint32 {
	if d <= 0 {
		return 0
	}
	ms := d / time.Millisecond
	if ms > (1<<32)-1 {
		return (1 << 32) - 1
	}
	return uint32(ms)
}

func xlateErrno(errno syscall.Errno) error {
	switch errno {
	case syscall.ETIMEDOUT:
		return ErrTimeout
	case syscall.EPIPE:
		return ErrStalled
	default:
		return errno
	}
}

// --- sysfs helpers ----------------------------------------------------------

func readString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readDec(path string) (int, bool) {
	s := readString(path)
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func readHex16(path string) (uint16, bool) {
	s := readString(path)
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(n), true
}

func readHex8(path string) (uint8, bool) {
	s := readString(path)
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 8)
	if err != nil {
		return 0, false
	}
	return uint8(n), true
}
