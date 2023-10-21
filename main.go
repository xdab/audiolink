package main

import (
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"syscall"

	"github.com/xdab/audiolink/audio"
	"github.com/xdab/audiolink/udp"
)

func main() {
	ip := flag.Int("ip", 4810, "incoming audio port")
	oh := flag.String("oh", "127.0.0.1", "outgoing audio host")
	op := flag.Int("op", 3810, "outgoing audio port")
	r := flag.Int("r", 8000, "audio sample rate")
	fs := flag.Int("fs", 320, "audio frame size")

	flag.Parse()

	// Incoming audio
	incomingAudioPort := *ip
	udprx := udp.CreateUDPRX(incomingAudioPort)
	err := udprx.Open()
	if err != nil {
		log.Fatalln(err)
	}
	udprx.StartReceiving()

	// Outgoing audio
	outgoingAudioHost := *oh
	outgoingAudioPort := *op
	udptx := udp.CreateUDPTX(outgoingAudioHost, outgoingAudioPort)
	err = udptx.Open()
	if err != nil {
		log.Fatalln(err)
	}

	// Virtual soundcard
	audioSampleRate := *r
	audioFrameSize := *fs
	virtualSoundcard := &audio.VirtualSoundcard{}
	virtualSoundcard.Init("audiolink", audioSampleRate, audioFrameSize)

	// Feed received audio to virtual microphone
	udprx.ReceivedDataCallback(func(data []byte) {
		log.Println("Received", len(data), "bytes")
		virtualSoundcard.Microphone <- data
	})

	// Send virtual speaker audio to remote host
	for data := range virtualSoundcard.Speaker {
		rms := rms(data)
		if rms > 1 {
			log.Println("RMS", rms)
			udptx.Send(data)
		}
	}

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Shutting down gracefully")
		udptx.Close()
		udprx.Close()
		virtualSoundcard.Close()
		os.Exit(1)
	}()

	select {}
}

func rms(data []byte) float64 {
	var sum float64
	samples := len(data) / 2
	for i := 0; i < len(data); i += 2 {
		v := float64(int16(data[i]) | int16(data[i+1])<<8)
		sum += v * v
	}
	return math.Sqrt(sum / float64(samples))
}
