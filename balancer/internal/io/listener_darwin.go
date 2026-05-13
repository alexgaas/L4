//go:build darwin
// +build darwin

package io

import (
	"balancer/internal/util"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"balancer/internal/log"
)

var Log log.Logger

type RecvMmsgData struct {
	Addr    syscall.RawSockaddrAny
	Data    [util.MaxBufferSize]byte
	DataLen uint32
}

type PlainUDPSocket struct {
	Fd       int
	UDPAddr  *net.UDPAddr
	KqueueFd int
	IsIPv6   bool
}

type PlainMultiSocket struct {
	v4 *PlainUDPSocket
	v6 *PlainUDPSocket
}

func NewPlainUDPSocket(addr string, isIPv6 bool) (*PlainUDPSocket, error) {
	host, portString, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	port, err := strconv.ParseInt(portString, 10, 16)
	if err != nil {
		return nil, err
	}

	af := syscall.AF_INET6
	var sockAddr syscall.Sockaddr
	if isIPv6 {
		addrBuff := syscall.SockaddrInet6{Port: int(port)}
		copy(addrBuff.Addr[:], net.ParseIP(host).To16())
		sockAddr = &addrBuff
	} else {
		af = syscall.AF_INET
		addrBuff := syscall.SockaddrInet4{Port: int(port)}
		copy(addrBuff.Addr[:], net.ParseIP(host).To4())
		sockAddr = &addrBuff
	}

	syscall.ForkLock.RLock()
	socket, err := syscall.Socket(af, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(socket)
	syscall.ForkLock.RUnlock()

	if err = syscall.SetNonblock(socket, true); err != nil {
		return nil, err
	}

	if err = syscall.SetsockoptInt(socket, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return nil, err
	}

	// do not mix IPv4 and IPv6 on the same IPv6 socket
	if isIPv6 {
		if err = syscall.SetsockoptInt(socket, syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, 1); err != nil {
			return nil, err
		}
	}

	if err := syscall.Bind(socket, sockAddr); err != nil {
		return nil, err
	}

	bindSockAddr, err := syscall.Getsockname(socket)
	if err != nil {
		return nil, err
	}

	var udpAddr *net.UDPAddr
	switch sa := bindSockAddr.(type) {
	case *syscall.SockaddrInet4:
	case *syscall.SockaddrInet6:
		udpAddr = &net.UDPAddr{IP: sa.Addr[:], Port: sa.Port}
	default:
		if isIPv6 {
			return nil, fmt.Errorf("could not get IPv6 bind address for %s", addr)
		} else {
			return nil, fmt.Errorf("could not get IPv4 bind address for %s", addr)
		}
	}

	kq, err := syscall.Kqueue()
	if err != nil {
		return nil, err
	}

	ev := syscall.Kevent_t{
		Ident:  uint64(socket),
		Filter: syscall.EVFILT_READ,
		Flags:  syscall.EV_ADD | syscall.EV_ENABLE,
	}

	if _, err = syscall.Kevent(kq, []syscall.Kevent_t{ev}, nil, nil); err != nil {
		_ = syscall.Close(kq)
		return nil, err
	}

	return &PlainUDPSocket{Fd: socket, UDPAddr: udpAddr, KqueueFd: kq}, nil
}

func (t *PlainUDPSocket) Read(mmsgs []MmsgHdr, mmsgcnt int) (int, error) {
	return RecvMMsg(t.Fd, mmsgs, mmsgcnt)
}

func (t *PlainUDPSocket) Close() {
	_ = syscall.Close(t.KqueueFd)
	_ = syscall.Close(t.Fd)
}

func NewPlainMultiSocket(addr string) (*PlainMultiSocket, error) {
	v4, err := NewPlainUDPSocket(addr, false)
	if err != nil {
		return nil, err
	}
	v6, err := NewPlainUDPSocket(addr, true)
	if err != nil {
		v4.Close()
		return nil, err
	}

	return &PlainMultiSocket{v4: v4, v6: v6}, nil
}

func (t *PlainMultiSocket) Close() {
	t.v4.Close()
	t.v6.Close()
}

func StartUDPListener(stopChan chan interface{}, wg *sync.WaitGroup, addr string, broadcaster Broadcaster, verbose bool) error {
	Log.Info("Udp server", log.Any("address", addr))
	plainSocket, err := NewPlainMultiSocket(addr)
	if err != nil {
		return err
	}

	listen := func(isIPv6 bool) {
		defer wg.Done()

		backend := broadcaster.Copy()

		err = backend.InitState()
		if err != nil {
			Log.Fatal("Error constructing msghdrs", log.Any("addr", addr), log.Error(err))
		}

		var socket *PlainUDPSocket
		if isIPv6 {
			socket = plainSocket.v6
		} else {
			socket = plainSocket.v4
		}

		var buffer [util.MaxReadFrames]RecvMmsgData
		var mmsgs [util.MaxReadFrames]MmsgHdr

		for i := 0; i < util.MaxReadFrames; i++ {
			var iov syscall.Iovec
			iov.Base = &buffer[i].Data[0]
			iov.SetLen(util.MaxBufferSize)

			var msg syscall.Msghdr
			msg.Iov = &iov
			msg.Iovlen = 1

			msg.Name = (*byte)(unsafe.Pointer(&buffer[i].Addr))
			// unix.SizeofSockaddrDatalink is typically 20 on Darwin/amd64 and arm6
			msg.Namelen = 108 // uint32(syscall.SizeofSockaddrLinklayer)

			mmsgs[i].MsgHdr = msg
			mmsgs[i].MsgLen = 1
		}

		var events [util.MaxEpollEvents]syscall.Kevent_t

		for {
			select {
			case <-stopChan:
				plainSocket.Close()
				return
			default:
			}

			n, err := syscall.Kevent(socket.KqueueFd, nil, events[:], nil)
			if errors.Is(err, syscall.EINTR) && n < 0 {
				continue
			} else if err != nil {
				Log.Error("Kqueue error", log.Error(err))
				os.Exit(1)
			}

			for ev := 0; ev < n; ev++ {
				if int(events[ev].Ident) == socket.Fd {
					sz, err := socket.Read(mmsgs[:], util.MaxReadFrames)
					if err != nil && sz < 0 && !errors.Is(err, syscall.EAGAIN) {
						Log.Error("Error reading from socket", log.Any("addr", addr), log.Error(err))
						continue
					}

					if sz < 1 || errors.Is(err, syscall.EAGAIN) {
						continue
					}

					for i := 0; i < sz; i++ {
						buffer[i].DataLen = mmsgs[i].MsgLen
					}

					backend.Enqueue(buffer[:], sz, isIPv6)
				}
			}
		}
	}

	wg.Add(1)
	go listen(false)
	wg.Add(1)
	go listen(true)
	wg.Add(1)
	go listen(false)
	wg.Add(1)
	go listen(true)

	return nil
}
