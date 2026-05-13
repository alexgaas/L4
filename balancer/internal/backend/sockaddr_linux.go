//go:build linux
// +build linux

package backend

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

func SetIPv4HeaderFields(data []byte, totalLen uint16) {
	// Linux expects network byte order (big-endian) for most fields
	binary.BigEndian.PutUint16(data[2:4], totalLen)
	binary.BigEndian.PutUint16(data[4:6], 0x1234)
	binary.BigEndian.PutUint16(data[6:8], 0x4000) // DF bit
}

func rawSocketAnyToInet4Pointer(sa *syscall.RawSockaddrAny) (unsafe.Pointer, uint32) {
	addr := (*syscall.RawSockaddrInet4)(unsafe.Pointer(sa))
	addr.Family = syscall.AF_INET
	return unsafe.Pointer(sa), syscall.SizeofSockaddrInet4
}

func rawSocketAnyToInet6Pointer(sa *syscall.RawSockaddrAny) (unsafe.Pointer, uint32) {
	addr := (*syscall.RawSockaddrInet6)(unsafe.Pointer(sa))
	addr.Family = syscall.AF_INET6
	return unsafe.Pointer(sa), syscall.SizeofSockaddrInet6
}
