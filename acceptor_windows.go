// Copyright 2019 Andy Pan. All rights reserved.
// Copyright 2018 Joshua J Baker. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// +build windows

package gnet

import (
	"time"

	"github.com/panlibin/gnet/pool/bytebuffer"
)

func (svr *server) listenerRun() {
	var err error
	defer func() { svr.signalShutdown(err) }()
	var packet [0x10000]byte
	for {
		if svr.ln.pconn != nil {
			// Read data from UDP socket.
			n, addr, e := svr.ln.pconn.ReadFrom(packet[:])
			if e != nil {
				err = e
				return
			}
			buf := bytebuffer.Get()
			_, _ = buf.Write(packet[:n])

			el := svr.subLoopGroup.next()
			el.ch <- &udpIn{newUDPConn(el, svr.ln.lnaddr, addr, buf)}
		} else {
			// Accept TCP socket.
			conn, e := svr.ln.ln.Accept()
			if e != nil {
				err = e
				return
			}
			el := svr.subLoopGroup.next()
			c := newTCPConn(conn, el)
			el.ch <- c
			go func() {
				var packet [0x10000]byte
				for {
					n, err := c.conn.Read(packet[:])
					if err != nil {
						_ = c.conn.SetReadDeadline(time.Time{})
						el.ch <- &stderr{c, err}
						return
					}
					buf := bytebuffer.Get()
					_, _ = buf.Write(packet[:n])
					el.ch <- &tcpIn{c, buf}
				}
			}()
		}
	}
}
