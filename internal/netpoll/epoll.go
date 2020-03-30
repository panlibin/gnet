// Copyright 2019 Andy Pan. All rights reserved.
// Copyright 2017 Joshua J Baker. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// +build linux

package netpoll

import (
	"log"
	"unsafe"

	"github.com/panlibin/gnet/internal"
	"golang.org/x/sys/unix"
)

// Poller represents a poller which is in charge of monitoring file-descriptors.
type Poller struct {
	fd            int    // epoll fd
	wfd           int    // wake fd
	wfdBuf        []byte // wfd buffer to read packet
	asyncJobQueue internal.AsyncJobQueue
}

// OpenPoller instantiates a poller.
func OpenPoller() (*Poller, error) {
	poller := new(Poller)
	epollFD, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, err
	}
	poller.fd = epollFD
	r0, _, errno := unix.Syscall(unix.SYS_EVENTFD2, unix.O_CLOEXEC, unix.O_NONBLOCK, 0)
	if errno != 0 {
		_ = unix.Close(epollFD)
		return nil, errno
	}
	poller.wfd = int(r0)
	poller.wfdBuf = make([]byte, 8)
	if err = poller.AddRead(poller.wfd); err != nil {
		return nil, err
	}
	poller.asyncJobQueue = internal.NewAsyncJobQueue()
	return poller, nil
}

// Close closes the poller.
func (p *Poller) Close() error {
	if err := unix.Close(p.wfd); err != nil {
		return err
	}
	return unix.Close(p.fd)
}

// Make the endianness of bytes compatible with more linux OSs under different processor-architectures,
// according to http://man7.org/linux/man-pages/man2/eventfd.2.html.
var (
	u uint64 = 1
	b        = (*(*[8]byte)(unsafe.Pointer(&u)))[:]
)

// Trigger wakes up the poller blocked in waiting for network-events and runs jobs in asyncJobQueue.
func (p *Poller) Trigger(job internal.Job) error {
	if p.asyncJobQueue.Push(job) == 1 {
		_, err := unix.Write(p.wfd, b)
		return err
	}
	return nil
}

// Polling blocks the current goroutine, waiting for network-events.
func (p *Poller) Polling(callback func(fd int, ev uint32) error) (err error) {
	el := newEventList(InitEvents)
	var wakenUp bool
	for {
		n, err0 := unix.EpollWait(p.fd, el.events, -1)
		if err0 != nil && err0 != unix.EINTR {
			log.Println(err0)
			continue
		}
		for i := 0; i < n; i++ {
			if fd := int(el.events[i].Fd); fd != p.wfd {
				if err = callback(fd, el.events[i].Events); err != nil {
					return
				}
			} else {
				wakenUp = true
				_, _ = unix.Read(p.wfd, p.wfdBuf)
			}
		}
		if wakenUp {
			wakenUp = false
			if err = p.asyncJobQueue.ForEach(); err != nil {
				return
			}
		}
		if n == el.size {
			el.increase()
		}
	}
}

const (
	readEvents      = unix.EPOLLPRI | unix.EPOLLIN
	writeEvents     = unix.EPOLLOUT
	readWriteEvents = readEvents | writeEvents
)

// AddReadWrite registers the given file-descriptor with readable and writable events to the poller.
func (p *Poller) AddReadWrite(fd int) error {
	return unix.EpollCtl(p.fd, unix.EPOLL_CTL_ADD, fd, &unix.EpollEvent{Fd: int32(fd), Events: readWriteEvents})
}

// AddRead registers the given file-descriptor with readable event to the poller.
func (p *Poller) AddRead(fd int) error {
	return unix.EpollCtl(p.fd, unix.EPOLL_CTL_ADD, fd, &unix.EpollEvent{Fd: int32(fd), Events: readEvents})
}

// AddWrite registers the given file-descriptor with writable event to the poller.
func (p *Poller) AddWrite(fd int) error {
	return unix.EpollCtl(p.fd, unix.EPOLL_CTL_ADD, fd, &unix.EpollEvent{Fd: int32(fd), Events: writeEvents})
}

// ModRead renews the given file-descriptor with readable event in the poller.
func (p *Poller) ModRead(fd int) error {
	return unix.EpollCtl(p.fd, unix.EPOLL_CTL_MOD, fd, &unix.EpollEvent{Fd: int32(fd), Events: readEvents})
}

// ModReadWrite renews the given file-descriptor with readable and writable events in the poller.
func (p *Poller) ModReadWrite(fd int) error {
	return unix.EpollCtl(p.fd, unix.EPOLL_CTL_MOD, fd, &unix.EpollEvent{Fd: int32(fd), Events: readWriteEvents})
}

// Delete removes the given file-descriptor from the poller.
func (p *Poller) Delete(fd int) error {
	return unix.EpollCtl(p.fd, unix.EPOLL_CTL_DEL, fd, nil)
}
