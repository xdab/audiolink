package audio

import (
	"bytes"
	"io"
	"log"
	"os"
	"sync"

	"github.com/akosmarton/papipes"
)

const SAMPLE_FORMAT = "s16le"
const SAMPLE_BYTES = 2

type VirtualSoundcard struct {
	Microphone chan []byte
	Speaker    chan []byte

	deviceName string
	sampleRate int
	frameSize  int
	bufferSize int

	stopSignal chan bool
	stopped    chan bool

	virtualMicrophone papipes.Source
	virtualSpeaker    papipes.Sink
	mutex             sync.Mutex
	playBuffer        *bytes.Buffer
	playBufferMaxSize int
	canPlay           chan bool
}

func (a *VirtualSoundcard) virtualMicrophoneFeeder(stopSignal, stopped chan bool) {
	for {
		select {
		case <-a.canPlay:
		case <-stopSignal:
			stopped <- true
			return
		}

		for {
			a.mutex.Lock()
			if a.playBuffer.Len() < a.frameSize {
				a.mutex.Unlock()
				break
			}

			d := make([]byte, a.frameSize)
			bytesToWrite, err := a.playBuffer.Read(d)

			a.mutex.Unlock()
			if err != nil {
				log.Println(err)
				break
			}
			if bytesToWrite != len(d) {
				log.Println("Buffer underread")
				break
			}

			for len(d) > 0 {
				written, err := a.virtualMicrophone.Write(d)
				if err != nil {
					if _, ok := err.(*os.PathError); !ok {
						log.Println(err)
					}
					break
				}
				d = d[written:]
			}
		}
	}
}

func (a *VirtualSoundcard) virtualSpeakerConsumer(stopSignal, stopped chan bool) {
	defer func() {
		stopped <- true
	}()

	frameBuf := make([]byte, a.frameSize)
	buf := bytes.NewBuffer([]byte{})

	for {
		select {
		case <-stopSignal:
			return
		default:
		}

		n, err := a.virtualSpeaker.Read(frameBuf)
		if err != nil {
			if _, ok := err.(*os.PathError); !ok {
				log.Println(err)
				if err == io.EOF {
					<-stopSignal
					return
				}
			}
		}

		buf.Write(frameBuf[:n])

		for buf.Len() >= len(frameBuf) {
			b := make([]byte, len(frameBuf))
			n, err = buf.Read(b)
			if err != nil {
				log.Println(err)
			}
			if n != len(frameBuf) {
				log.Println(err)
			}

			select {
			case a.Speaker <- b:
			case <-stopSignal:
				return
			}
		}
	}
}

func (a *VirtualSoundcard) loop() {
	virtualMicrophoneFeederStop := make(chan bool)
	virtualMicrophoneFeederStopped := make(chan bool)
	go a.virtualMicrophoneFeeder(virtualMicrophoneFeederStop, virtualMicrophoneFeederStopped)

	virtualSpeakerConsumerStop := make(chan bool)
	virtualSpeakerConsumerStopped := make(chan bool)
	go a.virtualSpeakerConsumer(virtualSpeakerConsumerStop, virtualSpeakerConsumerStopped)

	var d []byte
	for {
		select {
		case d = <-a.Microphone:
		case <-a.stopSignal:
			a.close()

			virtualSpeakerConsumerStop <- true
			<-virtualSpeakerConsumerStopped

			virtualMicrophoneFeederStop <- true
			<-virtualMicrophoneFeederStopped

			a.stopped <- true
			return
		}

		a.mutex.Lock()
		free := a.playBufferMaxSize - a.playBuffer.Len()
		if free < len(d) {
			b := make([]byte, len(d)-free)
			_, _ = a.playBuffer.Read(b)
		}
		a.playBuffer.Write(d)
		a.mutex.Unlock()

		select {
		case a.canPlay <- true:
		default:
		}
	}
}

func (a *VirtualSoundcard) Init(devName string, audioSampleRate int, audioFrameSize int) error {
	a.deviceName = devName

	a.sampleRate = audioSampleRate
	a.frameSize = audioFrameSize
	a.playBufferMaxSize = 10 * a.frameSize
	a.bufferSize = a.playBufferMaxSize * SAMPLE_BYTES

	if !a.virtualMicrophone.IsOpen() {
		a.virtualMicrophone.Name = a.deviceName
		a.virtualMicrophone.Filename = "/tmp/audiolink-" + a.deviceName + ".source"
		a.virtualMicrophone.Rate = audioSampleRate
		a.virtualMicrophone.Format = SAMPLE_FORMAT
		a.virtualMicrophone.Channels = 1
		a.virtualMicrophone.SetProperty("device.buffering.buffer_size", a.bufferSize)
		a.virtualMicrophone.SetProperty("device.description", "audiolink: "+a.deviceName)

		// Cleanup previous pipes.
		sources, err := papipes.GetActiveSources()
		if err == nil {
			for _, i := range sources {
				if i.Filename == a.virtualMicrophone.Filename {
					i.Close()
				}
			}
		}

		if err := a.virtualMicrophone.Open(); err != nil {
			return err
		}
	}

	if !a.virtualSpeaker.IsOpen() {
		a.virtualSpeaker.Name = a.deviceName
		a.virtualSpeaker.Filename = "/tmp/audiolink-" + a.deviceName + ".sink"
		a.virtualSpeaker.Rate = audioSampleRate
		a.virtualSpeaker.Format = SAMPLE_FORMAT
		a.virtualSpeaker.Channels = 1
		a.virtualSpeaker.UseSystemClockForTiming = true
		a.virtualSpeaker.SetProperty("device.buffering.buffer_size", a.bufferSize)
		a.virtualSpeaker.SetProperty("device.description", "audiolink: "+a.deviceName)

		// Cleanup previous pipes.
		sinks, err := papipes.GetActiveSinks()
		if err == nil {
			for _, i := range sinks {
				if i.Filename == a.virtualSpeaker.Filename {
					i.Close()
				}
			}
		}

		if err := a.virtualSpeaker.Open(); err != nil {
			return err
		}
	}

	if a.playBuffer == nil {
		log.Print("Opened device " + a.virtualMicrophone.Name)

		a.Microphone = make(chan []byte)
		a.Speaker = make(chan []byte)

		a.playBuffer = bytes.NewBuffer([]byte{})
		a.canPlay = make(chan bool)

		a.stopSignal = make(chan bool)
		a.stopped = make(chan bool)
		go a.loop()
	}
	return nil
}

func (a *VirtualSoundcard) close() {
	if a.virtualMicrophone.IsOpen() {
		if err := a.virtualMicrophone.Close(); err != nil {
			if _, ok := err.(*os.PathError); !ok {
				log.Println(err)
			}
		}
	}

	if a.virtualSpeaker.IsOpen() {
		if err := a.virtualSpeaker.Close(); err != nil {
			if _, ok := err.(*os.PathError); !ok {
				log.Println(err)
			}
		}
	}
}

func (a *VirtualSoundcard) Close() {
	a.close()
	if a.stopSignal != nil {
		a.stopSignal <- true
		<-a.stopped
	}
}
