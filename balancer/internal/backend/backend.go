package backend

import (
	"balancer/internal/io"
	"balancer/internal/log"
	"balancer/internal/util"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"go.uber.org/atomic"
)

var Log log.Logger

type HostSpecificInfo struct {
	Addr            string
	IP              []byte
	Port            int
	FakeSrcIPv4Addr *syscall.RawSockaddrInet4
	RawSockAddr     syscall.RawSockaddrAny
	Fd              int
	IsIPv6          bool
}

type Backend struct {
	UUID            string
	Host            *HostSpecificInfo
	PreserveSrcAddr bool
	ResolveOnlyIPV6 bool
	Verbose         bool
	Buffer          [util.MaxReadFrames]SendMMsgData
	Mmsgs           [util.MaxReadFrames]io.MmsgHdr
	PseudoHeader    [headerSizeIPv6Pseudo + util.MaxBufferSize]byte

	Weight     float64
	NewWeight  *atomic.Float64
	Seed       []byte
	CheckURL   string
	HTTPClient *http.Client
	Alive      atomic.Bool
}

func (b *Backend) InitState() error {
	var fd int
	var err error
	if b.Host.IsIPv6 {
		syscall.ForkLock.RLock()
		fd, err = syscall.Socket(syscall.AF_INET6, syscall.SOCK_RAW, syscall.IPPROTO_IPV6)
		syscall.CloseOnExec(fd)
		syscall.ForkLock.RUnlock()

		if err != nil {
			return err
		}

		if err = syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, IPV6HdrIncl, 1); err != nil {
			return err
		}
	} else {
		syscall.ForkLock.RLock()
		fd, err = syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
		syscall.CloseOnExec(fd)
		syscall.ForkLock.RUnlock()

		if err != nil {
			return err
		}

		if err = syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_HDRINCL, 1); err != nil {
			return err
		}
	}

	b.Host.Fd = fd

	for i := 0; i < util.MaxReadFrames; i++ {
		b.Buffer[i].Iov.Base = &b.Buffer[i].Data[0]
		b.Buffer[i].Iov.SetLen(b.Buffer[i].DataLen)

		b.Buffer[i].Msghdr.Iov = &b.Buffer[i].Iov
		b.Buffer[i].Msghdr.Iovlen = 1

		if b.Host.IsIPv6 {
			ptr, size := rawSocketAnyToInet6Pointer(&b.Host.RawSockAddr)
			b.Buffer[i].Msghdr.Name = (*byte)(ptr)
			b.Buffer[i].Msghdr.Namelen = size
		} else {
			ptr, size := rawSocketAnyToInet4Pointer(&b.Host.RawSockAddr)
			b.Buffer[i].Msghdr.Name = (*byte)(ptr)
			b.Buffer[i].Msghdr.Namelen = size
		}

		b.Mmsgs[i].MsgHdr = b.Buffer[i].Msghdr
		b.Mmsgs[i].MsgLen = 1
	}

	return nil
}

func (b *Backend) Enqueue(message []io.RecvMmsgData, cnt int, isIPv6 bool) {
	if !b.Host.IsIPv6 && isIPv6 && b.Host.FakeSrcIPv4Addr == nil {
		Log.Errorf("Got IPv6 packets for IPv4 destination %s but fake address not specified", b.Host.Addr)
		return
	}

	for i := 0; i < cnt; i++ {
		err := PrepareIPUDP(&b.Buffer[i], &message[i], b.Host, isIPv6, b.PseudoHeader)
		if err != nil {
			Log.Error("Error preparing raw packet", log.Error(err))
		}
		b.Buffer[i].Iov.SetLen(b.Buffer[i].DataLen)
		b.Buffer[i].DataLen = 0
	}

	_, err := io.SendMMsg(b.Host.Fd, b.Mmsgs[:], cnt)
	if err != nil {
		Log.Error("Error sending data to backend", log.Any("backend", b.Host.Addr), log.Error(err))
	}
}

func (b *Backend) IsAlive() bool {
	if b.HTTPClient == nil {
		dialWrapper := func(ctx context.Context, network, address string) (net.Conn, error) {
			if b.ResolveOnlyIPV6 {
				switch network {
				case "tcp":
					return net.Dial("tcp6", address)
				case "udp":
					return net.Dial("udp6", address)
				case "ip":
					return net.Dial("ip6", address)
				}
			}
			return net.Dial(network, address)
		}
		tr := &http.Transport{
			DialContext:        dialWrapper,
			DisableCompression: true,
			DisableKeepAlives:  true,
		}

		b.HTTPClient = &http.Client{
			Transport: tr,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout: time.Second * 10,
		}
	}

	resp, err := b.HTTPClient.Get(b.CheckURL)
	b.Alive.Store(true)
	if err != nil {
		Log.Error("Check URL", log.Any("url", b.CheckURL), log.Error(err))
		b.Alive.Store(false)
	} else if resp.StatusCode != 200 {
		Log.Error("Check URL", log.Any("url", b.CheckURL), log.Any("bad status code", resp.StatusCode))
		b.Alive.Store(false)
	}

	return b.Alive.Load()
}

func (b *Backend) Copy() io.Sender {
	backend := Backend{
		UUID:            b.UUID,
		Host:            b.Host,
		PreserveSrcAddr: b.PreserveSrcAddr,
		ResolveOnlyIPV6: b.ResolveOnlyIPV6,
		Verbose:         b.Verbose,

		// Right now we don't need to copy buffers
		// Initialization will be completed in listener
		// by InitState

		Weight:    b.Weight,
		NewWeight: b.NewWeight,
		Seed:      b.Seed,
		CheckURL:  b.CheckURL,
	}

	return &backend
}

func CreateHostSpecificInfo(addr string, isIPv6Only bool, fakeBackend string) (*HostSpecificInfo, error) {
	host, ports, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	port, err := strconv.ParseInt(ports, 10, 32)
	if err != nil {
		return nil, err
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errors.New(fmt.Sprint("unable to createHostSpecificInfo addr ", addr))
	}

	var ipAddr []byte
	for _, ipr := range ips {
		if ip := ipr.To4(); ip != nil && ipAddr == nil && !isIPv6Only {
			ipAddr = ipr.To4()
		} else if ipAddr == nil {
			ipAddr = ipr.To16()
		} else {
			break
		}
	}

	if ipAddr == nil {
		return nil, fmt.Errorf("could not find valid addr for %s IPv6 only %v", addr, isIPv6Only)
	}

	isIPv6 := len(ipAddr) == 16

	var sockAddr *syscall.RawSockaddrAny
	if isIPv6 {
		destAddr := syscall.RawSockaddrInet6{}
		copy(destAddr.Addr[:], ipAddr)
		destAddr.Port = 0 // Do not use real port. IPv6 will fail with invalid argument

		sockAddr = (*syscall.RawSockaddrAny)(unsafe.Pointer(&destAddr))
	} else {
		destAddr := syscall.RawSockaddrInet4{}
		copy(destAddr.Addr[:], ipAddr)
		destAddr.Port = uint16(port)

		sockAddr = (*syscall.RawSockaddrAny)(unsafe.Pointer(&destAddr))
	}

	var fakeAddr *syscall.RawSockaddrInet4
	if len(fakeBackend) > 0 {
		fakeAddr = &syscall.RawSockaddrInet4{}

		copy(fakeAddr.Addr[:], net.ParseIP(fakeBackend).To4())
		fakeAddr.Port = uint16(0)
	}

	return &HostSpecificInfo{
		Addr: addr, IP: ipAddr, Port: int(port),
		RawSockAddr: *sockAddr, FakeSrcIPv4Addr: fakeAddr,
		Fd: -1, IsIPv6: isIPv6,
	}, nil
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
