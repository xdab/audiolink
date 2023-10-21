package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/xdab/audiolink/audio"
	"github.com/xdab/audiolink/udp"
)

func main() {
	ip := flag.Int("ip", 4810, "incoming audio port")
	// ir := flag.Int("ir", 8000, "incoming audio rate")
	oh := flag.String("oh", "127.0.0.1", "outgoing audio host")
	op := flag.Int("op", 3810, "outgoing audio port")
	// or := flag.Int("or", 8000, "outgoing audio rate")
	flag.Parse()

	// Incoming audio
	incomingAudioPort := *ip
	udprx := udp.CreateUDPRX(incomingAudioPort)
	err := udprx.Open()
	if err != nil {
		log.Default().Fatal(err)
	}
	udprx.ReceivedDataCallback(func(data []byte) {
		log.Default().Println("Received", len(data), "bytes")
	})
	udprx.Receive()

	// Outgoing audio
	outgoingAudioHost := *oh
	outgoingAudioPort := *op
	udptx := udp.CreateUDPTX(outgoingAudioHost, outgoingAudioPort)
	err = udptx.Open()
	if err != nil {
		log.Default().Fatal(err)
	}

	// Virtual soundcard
	virtualSoundcard := &audio.VirtualSoundcard{}
	virtualSoundcard.Init("audiolink")

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Default().Println("Shutting down gracefully")
		udptx.Close()
		udprx.Close()
		os.Exit(1)
	}()

	select {}
}
