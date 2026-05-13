//go:build darwin
// +build darwin

package backend

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

func SetIPv4HeaderFields(data []byte, totalLen uint16) {
	// Darwin/BSD quirks:
	// 1. ip_len must be in host byte order.
	binary.LittleEndian.PutUint16(data[2:4], totalLen)

	// 2. ip_id should be in network byte order (big-endian).
	binary.BigEndian.PutUint16(data[4:6], 0x1234)

	// 3. ip_off must be in host byte order.
	// 0x4000 (DF bit) in host byte order (LE) is 0x0040.
	binary.LittleEndian.PutUint16(data[6:8], 0x4000)
}

func rawSocketAnyToInet4Pointer(sa *syscall.RawSockaddrAny) (unsafe.Pointer, uint32) {
	addr := (*syscall.RawSockaddrInet4)(unsafe.Pointer(sa))
	addr.Family = syscall.AF_INET
	addr.Len = syscall.SizeofSockaddrInet4
	return unsafe.Pointer(sa), syscall.SizeofSockaddrInet4
}

func rawSocketAnyToInet6Pointer(sa *syscall.RawSockaddrAny) (unsafe.Pointer, uint32) {
	addr := (*syscall.RawSockaddrInet6)(unsafe.Pointer(sa))
	addr.Family = syscall.AF_INET6
	addr.Len = syscall.SizeofSockaddrInet6
	return unsafe.Pointer(sa), syscall.SizeofSockaddrInet6
}
