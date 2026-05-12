//go:build darwin
// +build darwin

package io

import (
        "errors"
        "syscall"
        "unsafe"
)

type MmsgHdr struct {
        MsgHdr  syscall.Msghdr
        MsgLen  uint32
        PadCgo0 [4]byte
}

func RecvMMsg(fd int, mmsgs []MmsgHdr, msgcnt int) (int, error) {
        for i := 0; i < msgcnt; i++ {
                // Important: Reset Namelen to the size of the Addr buffer before each call
                // because the kernel updates it with the actual address size.
                mmsgs[i].MsgHdr.Namelen = 108 // SizeofSockaddrAny

                r0, _, err := syscall.Syscall(syscall.SYS_RECVMSG, uintptr(fd), uintptr(unsafe.Pointer(&mmsgs[i].MsgHdr)), 0)
                if err != 0 {
                        if i > 0 && (errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)) {
                                return i, nil
                        }
                        return i, err
                }
                mmsgs[i].MsgLen = uint32(r0)
        }
        return msgcnt, nil
}

func SendMMsg(fd int, mmsgs []MmsgHdr, msgcnt int) (int, error) {
        for i := 0; i < msgcnt; i++ {
                r0, _, err := syscall.Syscall(syscall.SYS_SENDMSG, uintptr(fd), uintptr(unsafe.Pointer(&mmsgs[i].MsgHdr)), 0)
                if err != 0 {
                        if i > 0 {
                                return i, nil
                        }
                        return i, err
                }
                mmsgs[i].MsgLen = uint32(r0)
        }
        return msgcnt, nil
}
