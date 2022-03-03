package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type LiveStation struct {
	store       *LiveStationStore
	UserID      string
	ID          string
	Folder      string
	LastRequest time.Time
	Quit        chan struct{}
	Queue       LiveStreamQueue
}

type LiveStationStore struct {
	Stations []*LiveStation
	mu       sync.Mutex
}

func (store *LiveStationStore) Get(station *Station) *LiveStation {
	store.mu.Lock()
	defer store.mu.Unlock()

	for i := range store.Stations {
		if store.Stations[i].UserID == station.UserID && store.Stations[i].ID == station.ID {
			store.Stations[i].LastRequest = time.Now()
			return store.Stations[i]
		}
	}

	return nil
}

func (store *LiveStationStore) GetOrCreate(station *Station) *LiveStation {
	store.mu.Lock()
	defer store.mu.Unlock()

	for i := range store.Stations {
		if store.Stations[i].UserID == station.UserID && store.Stations[i].ID == station.ID {
			store.Stations[i].LastRequest = time.Now()
			return store.Stations[i]
		}
	}

	liveStation := &LiveStation{
		store:       store,
		UserID:      station.UserID,
		ID:          station.ID,
		Folder:      "LIVE_" + generateID(),
		LastRequest: time.Now(),
		Quit:        make(chan struct{}),
		Queue:       LiveStreamQueue{},
	}

	store.Stations = append(store.Stations, liveStation)

	liveStation.Start()

	return liveStation
}

func (store *LiveStationStore) GetByFolder(folder string) *LiveStation {
	store.mu.Lock()
	defer store.mu.Unlock()

	for i := range store.Stations {
		if store.Stations[i].Folder == folder {
			return store.Stations[i]
		}
	}

	return nil
}

func (store *LiveStationStore) Add(station *LiveStation) {
	store.mu.Lock()

	store.Stations = append(store.Stations, station)

	store.mu.Unlock()
}

func (store *LiveStationStore) Remove(station *LiveStation) {
	store.mu.Lock()
	defer store.mu.Unlock()

	for i := range store.Stations {
		if store.Stations[i] == station {
			store.Stations[i] = nil
			store.Stations = append(store.Stations[:i], store.Stations[i+1:]...)

			break
		}
	}
}

const TickLengthInSeconds = 2
const BytesPerSecond = 44100 /* sample rate */ * 2 /* 16-bit */ * 2 /* channels (stereo) */
const BytesPerTick = BytesPerSecond * TickLengthInSeconds

func (station *LiveStation) RunTicker(ffmpeg *exec.Cmd, stdin io.WriteCloser) {
	ticker := time.NewTicker(TickLengthInSeconds * time.Second)
	station.Quit = make(chan struct{}, 1)

	for {
		select {
		case <-ticker.C:
			frame, hasMore := station.Queue.GetAudioFrame()

			_, err := stdin.Write(frame)
			if err != nil {
				station.Quit <- struct{}{}
				break
			}

			if !hasMore && time.Until(station.LastRequest.Add(time.Second*8)) < 0 {
				station.Quit <- struct{}{}

				break
			}
		case <-station.Quit:
			ticker.Stop()

			_ = ffmpeg.Process.Kill()
			_ = os.RemoveAll("media/" + station.Folder)

			station.store.Remove(station)

			return
		}
	}
}

func (station *LiveStation) Start() {
	err := os.Mkdir("media/"+station.Folder, 0777)
	if err != nil {
		fmt.Println(err)
		return
	}

	ffmpeg := exec.Command("ffmpeg", "-f", "s16le", "-ar", "44100", "-ac", "2", "-i", "-", "-vn", "-hls_time", "2", "-hls_segment_type", "fmp4", "-hls_flags", "discont_start+delete_segments", "-c:a", "libfdk_aac", "-b:a", "192k", "-master_pl_name", "master.m3u8", "media/"+station.Folder+"/output.m3u8")

	stdin, err := ffmpeg.StdinPipe()

	if err != nil {
		return
	}

	go station.RunTicker(ffmpeg, stdin)

	err = ffmpeg.Start()
	if err != nil {
		panic(err)
	}

	_, _ = stdin.Write(make([]byte, BytesPerSecond*5)) // Write 5 seconds of silence
}
