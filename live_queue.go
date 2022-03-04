package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type StreamQueue struct {
	elements []*StreamQueueElement
	mu       sync.Mutex
}

func (queue *StreamQueue) Add(el *StreamQueueElement) {
	queue.mu.Lock()

	queue.elements = append(queue.elements, el)

	defer queue.mu.Unlock()
}

func (queue *StreamQueue) shift() {
	if len(queue.elements) > 0 {
		queue.elements[0] = nil
		queue.elements = queue.elements[1:]
	}
}

func (queue *StreamQueue) GetAudioFrame() ([]byte, bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()

	frame := make([]byte, BytesPerTick)

	if len(queue.elements) == 0 {
		return frame, false
	}

	if !queue.elements[0].started {
		go queue.elements[0].Start()
	}

	read, err := queue.elements[0].Read(frame)

	if len(queue.elements) >= 2 {
		if !queue.elements[1].started && (queue.elements[0].IsNearEnd() || err != nil) {
			go queue.elements[1].Start()
		}

		if read < BytesPerTick && errors.Is(err, io.EOF) {
			_, _ = queue.elements[1].Read(frame[read:])
		}
	}

	if errors.Is(err, io.EOF) {
		queue.shift()
	}

	return frame, true
}

type StreamQueueElement struct {
	source  string
	data    []byte
	started bool
	done    bool
	mu      sync.Mutex
}

func (e *StreamQueueElement) Start() {
	e.started = true
	master := "media/" + e.source + "/master.m3u8"

	for {
		// Make sure the media source exists
		if _, err := os.Stat("media/" + e.source); os.IsNotExist(err) {
			// We're just going to set e.done to true to tell the queue to move on, as something is wrong
			e.done = true

			return
		}

		if _, err := os.Stat(master); !os.IsNotExist(err) {
			// Master playlist found! Let's process it!
			break
		}

		// If we're here, it probably means ffmpeg isn't finished downloading the source, so we'll try again in a sec
		time.Sleep(time.Second)
	}

	command := exec.Command("ffmpeg", "-i", master, "-f", "s16le", "-ar", "44100", "-ac", "2", "pipe:1")

	pipe, err := command.StdoutPipe()
	if err != nil {
		e.done = true

		return
	}

	err = command.Start()
	if err != nil {
		e.done = true

		return
	}

	for {
		buf := make([]byte, 44100*4*2)
		read, err := pipe.Read(buf)

		e.mu.Lock()

		e.data = append(e.data, buf[:read]...)

		e.mu.Unlock()

		if err != nil {
			e.done = true

			return
		}
	}
}

func (e *StreamQueueElement) Read(b []byte) (n int, err error) {
	e.mu.Lock()

	n = copy(b, e.data)

	e.data = e.data[n:]

	if len(e.data) == 0 && e.done {
		err = io.EOF
	}

	e.mu.Unlock()

	return
}

func (e *StreamQueueElement) IsNearEnd() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return len(e.data) < BytesPerTick
}
