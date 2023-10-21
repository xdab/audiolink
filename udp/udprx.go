package udp

import (
	"log"
	"net"
)

type UDPRX struct {
	localPort  int
	udpServer  *net.UDPConn
	rxCallback func([]byte)
}

func CreateUDPRX(localPort int) *UDPRX {
	return &UDPRX{
		localPort: localPort,
	}
}

func (u *UDPRX) Open() error {
	var err error
	log.Default().Println("Opening UDP RX socket on port", u.localPort)
	u.udpServer, err = net.ListenUDP("udp", &net.UDPAddr{
		Port: u.localPort,
	})
	return err
}

func (u *UDPRX) ReceivedDataCallback(rxCallback func([]byte)) {
	u.rxCallback = rxCallback
}

func (u *UDPRX) Receive() {
	go func() {
		for {
			buf := make([]byte, 1024)
			n, _, err := u.udpServer.ReadFrom(buf)
			if err != nil {
				return
			}
			if u.rxCallback != nil {
				u.rxCallback(buf[:n])
			}
		}
	}()
}

func (u *UDPRX) Close() error {
	log.Default().Println("Closing UDP RX socket on port", u.localPort)
	return u.udpServer.Close()
}
