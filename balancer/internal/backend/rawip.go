package backend

import (
	"balancer/internal/io"
	"balancer/internal/util"
	"encoding/binary"
	"errors"
	"syscall"
	"unsafe"
)

const headerSizeIPv6 = 40
const headerSizeUDP = 8
const headerSizeIPv6Pseudo = 48
const headerUDP6PseudoPort = 40

const IPV6HdrIncl = 36

var IPv4Header = []byte{
	0x45, // version + IHL 4 << 4 + 5
	0x00, // DSCP + ECN
	0x00,
	0x00, // length
	0x00,
	0x00, // identification
	0x40,
	0x00, // 3 bit flag (0x400 - dont fragment) and 13 frag offset
	0x40, // ttl - 64
	0x11, // UDP protocol
	0x00,
	0x00, // headers checksum
}

var IPv6Header = []byte{
	0x60, // version and 4 bits traffic class
	0x00, // 4 bits traffic class, 4 bits flow label
	0x00,
	0x00, // next 8 + 8 bits flow label
	0x00,
	0x00, // replace length later
	0x11, // UDP protocol
	0x40, // ttl - 64
}

var IPv6NetPrefix = []byte{
	0x00, 0x64, 0xFF, 0x9B,
	0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00,
}

type SendMMsgData struct {
	Data    [headerSizeIPv6 + headerSizeUDP + util.MaxBufferSize]byte
	DataLen int
	Iov     syscall.Iovec
	Msghdr  syscall.Msghdr
}

func Checksum(buf []byte) uint16 {
	sum := uint32(0)

	for ; len(buf) >= 2; buf = buf[2:] {
		sum += uint32(buf[0])<<8 | uint32(buf[1])
	}
	if len(buf) > 0 {
		sum += uint32(buf[0]) << 8
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	csum := ^uint16(sum)
	/*
	 * From RFC 768:
	 * If the computed checksum is zero, it is transmitted as all ones (the
	 * equivalent in one's complement arithmetic). An all zero transmitted
	 * checksum value means that the transmitter generated no checksum (for
	 * debugging or for higher level protocols that don't care).
	 */
	if csum == 0 {
		csum = 0xffff
	}
	return csum
}

func PrepareIPUDP(dst *SendMMsgData, src *io.RecvMmsgData, b *HostSpecificInfo, isSrcIPv6 bool, pseudoHeaderBuf [headerSizeIPv6Pseudo + util.MaxBufferSize]byte) error {
	offset := 0
	pseudoOffset := 0

	if b.IsIPv6 {
		copy(dst.Data[offset:], IPv6Header)
		offset += len(IPv6Header)

		// replace payload length
		binary.BigEndian.PutUint32(dst.Data[offset-6:offset-2], src.DataLen+8)

		if !isSrcIPv6 {
			if src.Addr.Addr.Family == syscall.AF_INET {
				addr := (*syscall.RawSockaddrInet4)(unsafe.Pointer(&src.Addr))

				copy(dst.Data[offset:], IPv6NetPrefix)
				offset += len(IPv6NetPrefix)
				copy(pseudoHeaderBuf[pseudoOffset:], IPv6NetPrefix)
				pseudoOffset += len(IPv6NetPrefix)

				copy(dst.Data[offset:], addr.Addr[:])
				offset += 4
				copy(pseudoHeaderBuf[pseudoOffset:], addr.Addr[:])
				pseudoOffset += 4

				// addr is RawSocketInetAddr. Port param is already bigendian. To prevent double conversation LE is used.
				binary.LittleEndian.PutUint16(dst.Data[headerUDP6PseudoPort:headerUDP6PseudoPort+2], addr.Port)
				binary.LittleEndian.PutUint16(pseudoHeaderBuf[headerUDP6PseudoPort:headerUDP6PseudoPort+2], addr.Port)
			} else {
				return errors.New("could not convert src ip to ipv4")
			}
		} else {
			if src.Addr.Addr.Family == syscall.AF_INET6 {
				addr := (*syscall.RawSockaddrInet6)(unsafe.Pointer(&src.Addr))

				copy(dst.Data[offset:], addr.Addr[:])
				offset += 16
				copy(pseudoHeaderBuf[pseudoOffset:], addr.Addr[:])
				pseudoOffset += 16

				// Already bigendian
				binary.LittleEndian.PutUint16(dst.Data[headerUDP6PseudoPort:headerUDP6PseudoPort+2], addr.Port)
				binary.LittleEndian.PutUint16(pseudoHeaderBuf[headerUDP6PseudoPort:headerUDP6PseudoPort+2], addr.Port)
			} else {
				return errors.New("could not convert src ip to ipv4")
			}
		}
		copy(dst.Data[offset:], b.IP)
		// source port already in buffer
		offset = headerUDP6PseudoPort + 2
		copy(pseudoHeaderBuf[pseudoOffset:], b.IP)
		pseudoOffset += 16

		// UDP pseudoheader len has 32 bits size
		binary.BigEndian.PutUint32(pseudoHeaderBuf[pseudoOffset:pseudoOffset+4], src.DataLen+8)
		pseudoOffset += 4
		// Put first part of zeroes to pseudoheader
		binary.BigEndian.PutUint16(pseudoHeaderBuf[pseudoOffset:pseudoOffset+2], 0x0)
		pseudoOffset += 2
		// Put 4 bits of zeros and UDP protocol
		binary.BigEndian.PutUint16(pseudoHeaderBuf[pseudoOffset:pseudoOffset+2], 0x11)
		// source port already in pseudoheader buffer
		pseudoOffset = headerUDP6PseudoPort + 2

		binary.BigEndian.PutUint16(dst.Data[offset:offset+2], uint16(b.Port))
		offset += 2
		binary.BigEndian.PutUint16(pseudoHeaderBuf[pseudoOffset:pseudoOffset+2], uint16(b.Port))
		pseudoOffset += 2

		binary.BigEndian.PutUint16(dst.Data[offset:offset+2], uint16(src.DataLen+8))
		offset += 2
		binary.BigEndian.PutUint16(pseudoHeaderBuf[pseudoOffset:pseudoOffset+2], uint16(src.DataLen+8))
		// skip two bytes of checksum
		pseudoOffset += 4

		copy(pseudoHeaderBuf[pseudoOffset:], src.Data[:src.DataLen])
		pseudoOffset += int(src.DataLen)

		csum := Checksum(pseudoHeaderBuf[:pseudoOffset])
		binary.BigEndian.PutUint16(dst.Data[offset:offset+2], csum)
		offset += 2
		copy(dst.Data[offset:], src.Data[:src.DataLen])
		offset += int(src.DataLen)
		dst.DataLen = offset
	} else {
		if src.Addr.Addr.Family == syscall.AF_INET6 && b.FakeSrcIPv4Addr == nil {
			return errors.New("fake src ipv4 address is not specified for IPV6 port")
		}

		copy(dst.Data[offset:], IPv4Header)
		
		totalLen := uint16(20 + 8 + src.DataLen)
		SetIPv4HeaderFields(dst.Data[offset:], totalLen)

		offset += len(IPv4Header)

		var addr *syscall.RawSockaddrInet4
		if src.Addr.Addr.Family == syscall.AF_INET {
			addr = (*syscall.RawSockaddrInet4)(unsafe.Pointer(&src.Addr))
		} else {
			srcAddr := (*syscall.RawSockaddrInet6)(unsafe.Pointer(&src.Addr))
			addr = b.FakeSrcIPv4Addr
			addr.Port = srcAddr.Port
		}

		copy(dst.Data[offset:], addr.Addr[:])
		copy(pseudoHeaderBuf[pseudoOffset:], addr.Addr[:])
		offset += 4
		pseudoOffset += 4
		copy(dst.Data[offset:], b.IP[:])
		copy(pseudoHeaderBuf[pseudoOffset:], b.IP[:])
		pseudoOffset += 4
		offset += 4
		// Already bigendian
		binary.LittleEndian.PutUint16(dst.Data[offset:offset+2], addr.Port)
		offset += 2
		binary.BigEndian.PutUint16(dst.Data[offset:offset+2], uint16(b.Port))
		offset += 2
		binary.BigEndian.PutUint16(dst.Data[offset:offset+2], uint16(src.DataLen+8))
		offset += 2

		binary.BigEndian.PutUint16(pseudoHeaderBuf[pseudoOffset:pseudoOffset+2], syscall.IPPROTO_UDP)
		pseudoOffset += 2
		binary.BigEndian.PutUint16(pseudoHeaderBuf[pseudoOffset:pseudoOffset+2], uint16(src.DataLen+8))
		pseudoOffset += 2
		// Already bigendian
		binary.LittleEndian.PutUint16(pseudoHeaderBuf[pseudoOffset:pseudoOffset+2], addr.Port)
		pseudoOffset += 2
		binary.BigEndian.PutUint16(pseudoHeaderBuf[pseudoOffset:pseudoOffset+2], uint16(b.Port))
		pseudoOffset += 2
		binary.BigEndian.PutUint16(pseudoHeaderBuf[pseudoOffset:pseudoOffset+2], uint16(src.DataLen+8))
		// skip two bytes of checksum
		pseudoOffset += 4
		copy(pseudoHeaderBuf[pseudoOffset:], src.Data[:src.DataLen])
		pseudoOffset += int(src.DataLen)

		csum := Checksum(pseudoHeaderBuf[:pseudoOffset])
		binary.BigEndian.PutUint16(dst.Data[offset:offset+2], csum)
		offset += 2
		copy(dst.Data[offset:], src.Data[:src.DataLen])
		offset += int(src.DataLen)
		dst.DataLen = offset
	}

	return nil
}
