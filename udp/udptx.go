package udp

import (
	"log"
	"net"
)

type UDPTX struct {
	remoteAddr *net.UDPAddr
	socket     *net.UDPConn
}

func CreateUDPTX(remoteHost string, remotePort int) *UDPTX {
	return &UDPTX{
		remoteAddr: &net.UDPAddr{
			IP:   net.ParseIP(remoteHost),
			Port: int(remotePort),
		},
	}
}

func (u *UDPTX) Open() error {
	var err error
	log.Println("Opening UDP TX socket on port", u.remoteAddr.Port)
	u.socket, err = net.DialUDP("udp", nil, u.remoteAddr)
	return err
}

func (u *UDPTX) Send(data []byte) (int, error) {
	log.Println("Sending", len(data), "bytes")
	return u.socket.Write(data)
}

func (u *UDPTX) Close() error {
	log.Println("Closing UDP TX socket on port", u.remoteAddr.Port)
	return u.socket.Close()
}
