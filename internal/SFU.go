package internal

import (
	"net"
)


func SFU(data []byte, targets []*net.UDPAddr, sender *net.UDPAddr, conn *net.UDPConn) error {
    for _, target := range targets {
        // Мы должны отправлять пакет ВСЕМ, КРОМЕ того, кто его прислал
        if target.String() == sender.String() {
            continue 
        }
        conn.WriteToUDP(data, target)
    }
	return nil
}