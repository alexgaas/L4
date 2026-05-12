// +build linux

package io

import (
        "syscall"
        "unsafe"
)

const SysSendmmsg = 307
const SysRecvmmsg = 299

type MmsgHdr struct {
        MsgHdr  syscall.Msghdr
        MsgLen  uint32
        PadCgo0 [4]byte
}

func RecvMMsg(fd int, mmsgs []MmsgHdr, msgcnt int) (int, error) {
        timeout := syscall.Timespec{Sec: 1, Nsec: 0}

        r0, _, err := syscall.Syscall6(SysRecvmmsg, uintptr(fd), uintptr(unsafe.Pointer(&mmsgs[0])), uintptr(msgcnt), 0, uintptr(unsafe.Pointer(&timeout)), 0)
        return int(r0), err
}

func SendMMsg(fd int, mmsgs []MmsgHdr, msgcnt int) (int, error) {
        r0, _, err := syscall.Syscall6(SysSendmmsg, uintptr(fd), uintptr(unsafe.Pointer(&mmsgs[0])), uintptr(msgcnt), 0, 0, 0)
        return int(r0), err
}
