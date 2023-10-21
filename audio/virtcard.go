package audio

import (
	"bytes"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/akosmarton/papipes"
)

const audioSampleRate = 8000
const audioSampleBytes = 2
const pulseAudioBufferLength = 100 * time.Millisecond
const audioFrameLength = 20 * time.Millisecond
const audioFrameSize = int((audioSampleRate * audioSampleBytes * audioFrameLength) / time.Second)
const audioRxSeqBufLength = 10
const maxPlayBufferSize = audioFrameSize*5 + int((audioSampleRate*audioSampleBytes*audioRxSeqBufLength)/time.Second)

type VirtualSoundcard struct {
	devName string

	deinitNeededChan   chan bool
	deinitFinishedChan chan bool

	// Send to this channel to play audio.
	play chan []byte

	// Read from this channel for audio.
	rec chan []byte

	virtualSoundcardStream struct {
		source papipes.Source
		sink   papipes.Sink

		mutex   sync.Mutex
		playBuf *bytes.Buffer
		canPlay chan bool
	}
}

func (a *VirtualSoundcard) playLoopToVirtualSoundcard(deinitNeededChan, deinitFinishedChan chan bool) {
	for {
		select {
		case <-a.virtualSoundcardStream.canPlay:
		case <-deinitNeededChan:
			deinitFinishedChan <- true
			return
		}

		for {
			a.virtualSoundcardStream.mutex.Lock()
			if a.virtualSoundcardStream.playBuf.Len() < audioFrameSize {
				a.virtualSoundcardStream.mutex.Unlock()
				break
			}

			d := make([]byte, audioFrameSize)
			bytesToWrite, err := a.virtualSoundcardStream.playBuf.Read(d)
			a.virtualSoundcardStream.mutex.Unlock()
			if err != nil {
				log.Println(err)
				break
			}
			if bytesToWrite != len(d) {
				log.Println("buffer underread")
				break
			}

			for len(d) > 0 {
				written, err := a.virtualSoundcardStream.source.Write(d)
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

func (a *VirtualSoundcard) recLoopFromVirtualSoundcard(deinitNeededChan, deinitFinishedChan chan bool) {
	defer func() {
		deinitFinishedChan <- true
	}()

	frameBuf := make([]byte, audioFrameSize)
	buf := bytes.NewBuffer([]byte{})

	for {
		select {
		case <-deinitNeededChan:
			return
		default:
		}

		n, err := a.virtualSoundcardStream.sink.Read(frameBuf)
		if err != nil {
			if _, ok := err.(*os.PathError); !ok {
				log.Println(err)
				if err == io.EOF {
					<-deinitNeededChan
					return
				}
			}
		}

		buf.Write(frameBuf[:n])

		for buf.Len() >= len(frameBuf) {
			// We need to create a new []byte slice for each chunk to be able to send it through the rec chan.
			b := make([]byte, len(frameBuf))
			n, err = buf.Read(b)
			if err != nil {
				log.Println(err)
			}
			if n != len(frameBuf) {
				log.Println(err)
			}

			select {
			case a.rec <- b:
			case <-deinitNeededChan:
				return
			}
		}
	}
}

func (a *VirtualSoundcard) loop() {
	playLoopToVirtualSoundcardDeinitNeededChan := make(chan bool)
	playLoopToVirtualSoundcardDeinitFinishedChan := make(chan bool)
	go a.playLoopToVirtualSoundcard(playLoopToVirtualSoundcardDeinitNeededChan, playLoopToVirtualSoundcardDeinitFinishedChan)

	recLoopFromVirtualSoundcardDeinitNeededChan := make(chan bool)
	recLoopFromVirtualSoundcardDeinitFinishedChan := make(chan bool)
	go a.recLoopFromVirtualSoundcard(recLoopFromVirtualSoundcardDeinitNeededChan, recLoopFromVirtualSoundcardDeinitFinishedChan)

	var d []byte
	for {
		select {
		case d = <-a.play:
		case <-a.deinitNeededChan:
			a.closeIfNeeded()

			recLoopFromVirtualSoundcardDeinitNeededChan <- true
			<-recLoopFromVirtualSoundcardDeinitFinishedChan
			playLoopToVirtualSoundcardDeinitNeededChan <- true
			<-playLoopToVirtualSoundcardDeinitFinishedChan

			a.deinitFinishedChan <- true
			return
		}

		a.virtualSoundcardStream.mutex.Lock()
		free := maxPlayBufferSize - a.virtualSoundcardStream.playBuf.Len()
		if free < len(d) {
			b := make([]byte, len(d)-free)
			_, _ = a.virtualSoundcardStream.playBuf.Read(b)
		}
		a.virtualSoundcardStream.playBuf.Write(d)
		a.virtualSoundcardStream.mutex.Unlock()

		// Non-blocking notify.
		select {
		case a.virtualSoundcardStream.canPlay <- true:
		default:
		}
	}
}

// We only init the audio once, with the first device name we acquire, so apps using the virtual sound card
// won't have issues with the interface going down while the app is running.
func (a *VirtualSoundcard) Init(devName string) error {
	a.devName = devName
	bufferSizeInBits := (audioSampleRate * audioSampleBytes * 8) / 1000 * pulseAudioBufferLength.Milliseconds()

	if !a.virtualSoundcardStream.source.IsOpen() {
		a.virtualSoundcardStream.source.Name = a.devName
		a.virtualSoundcardStream.source.Filename = "/tmp/audiolink-" + a.devName + ".source"
		a.virtualSoundcardStream.source.Rate = audioSampleRate
		a.virtualSoundcardStream.source.Format = "s16le"
		a.virtualSoundcardStream.source.Channels = 1
		a.virtualSoundcardStream.source.SetProperty("device.buffering.buffer_size", bufferSizeInBits)
		a.virtualSoundcardStream.source.SetProperty("device.description", "audiolink: "+a.devName)

		// Cleanup previous pipes.
		sources, err := papipes.GetActiveSources()
		if err == nil {
			for _, i := range sources {
				if i.Filename == a.virtualSoundcardStream.source.Filename {
					i.Close()
				}
			}
		}

		if err := a.virtualSoundcardStream.source.Open(); err != nil {
			return err
		}
	}

	if !a.virtualSoundcardStream.sink.IsOpen() {
		a.virtualSoundcardStream.sink.Name = a.devName
		a.virtualSoundcardStream.sink.Filename = "/tmp/audiolink-" + a.devName + ".sink"
		a.virtualSoundcardStream.sink.Rate = audioSampleRate
		a.virtualSoundcardStream.sink.Format = "s16le"
		a.virtualSoundcardStream.sink.Channels = 1
		a.virtualSoundcardStream.sink.UseSystemClockForTiming = true
		a.virtualSoundcardStream.sink.SetProperty("device.buffering.buffer_size", bufferSizeInBits)
		a.virtualSoundcardStream.sink.SetProperty("device.description", "audiolink: "+a.devName)

		// Cleanup previous pipes.
		sinks, err := papipes.GetActiveSinks()
		if err == nil {
			for _, i := range sinks {
				if i.Filename == a.virtualSoundcardStream.sink.Filename {
					i.Close()
				}
			}
		}

		if err := a.virtualSoundcardStream.sink.Open(); err != nil {
			return err
		}
	}

	if a.virtualSoundcardStream.playBuf == nil {
		log.Print("Opened device " + a.virtualSoundcardStream.source.Name)

		a.play = make(chan []byte)
		a.rec = make(chan []byte)

		a.virtualSoundcardStream.playBuf = bytes.NewBuffer([]byte{})
		a.virtualSoundcardStream.canPlay = make(chan bool)

		a.deinitNeededChan = make(chan bool)
		a.deinitFinishedChan = make(chan bool)
		go a.loop()
	}
	return nil
}

func (a *VirtualSoundcard) closeIfNeeded() {
	if a.virtualSoundcardStream.source.IsOpen() {
		if err := a.virtualSoundcardStream.source.Close(); err != nil {
			if _, ok := err.(*os.PathError); !ok {
				log.Println(err)
			}
		}
	}

	if a.virtualSoundcardStream.sink.IsOpen() {
		if err := a.virtualSoundcardStream.sink.Close(); err != nil {
			if _, ok := err.(*os.PathError); !ok {
				log.Println(err)
			}
		}
	}
}

func (a *VirtualSoundcard) Close() {
	a.closeIfNeeded()

	if a.deinitNeededChan != nil {
		a.deinitNeededChan <- true
		<-a.deinitFinishedChan
	}
}
