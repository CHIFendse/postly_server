package internal

import (
	"net"
)

func SFU(data []byte, targets []net.Conn, sender net.Conn) error {
	senderAddr := sender.RemoteAddr().String()
	for _, target := range targets {
		if target.RemoteAddr().String() == senderAddr {
			continue
		}
		target.Write(data)
	}
	return nil
}
