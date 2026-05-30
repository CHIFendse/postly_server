package sfu

import "net"

func Forward(data []byte, targets []net.Conn, sender net.Conn) {
	senderAddr := sender.RemoteAddr().String()
	for _, t := range targets {
		if t.RemoteAddr().String() != senderAddr {
			t.Write(data)
		}
	}
}
