package main

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	StationTypeStatic = "static"
	StationTypeLive   = "live"
)

func getDuration(output string) (int, error) {
	var duration float64

	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "#EXTINF:") {
			float, err := strconv.ParseFloat(line[8:len(line)-1], 64)
			if err != nil {
				return 0, err
			}

			duration += float
		}
	}

	return int(duration), nil
}

func (server *FNRadioServer) createBlurl(station *Station, c *gin.Context) ([]byte, error) {
	if c.Request.Header.Get("X-API-Root") == "" {
		return nil, errors.New("invalid api root")
	}

	if strings.EqualFold(station.Type, StationTypeStatic) {
		return server.createStaticBlurl(station, c)
	}

	if strings.EqualFold(station.Type, StationTypeLive) {
		return server.createLiveBlurl(station, c)
	}

	return nil, errors.New("unknown station type")
}

func (server *FNRadioServer) createStaticBlurl(station *Station, c *gin.Context) ([]byte, error) {
	master, err := os.ReadFile("media/" + station.Source.String + "/master.m3u8")
	if err != nil {
		return nil, err
	}

	output, err := os.ReadFile("media/" + station.Source.String + "/output.m3u8")
	if err != nil {
		return nil, err
	}

	duration, err := getDuration(string(output))
	if err != nil {
		return nil, err
	}

	mediaRoot := c.Request.Header.Get("X-API-Root") + "/media/" + url.PathEscape(station.Source.String)

	return encodeBlurl(&BLURL{
		Playlists: []Playlist{
			{
				Type:     "master",
				Language: "en",
				URL:      mediaRoot + "/master.m3u8",
				Data:     string(master),
				Duration: duration,
			},
			{
				Type:     "variant",
				Language: "en",
				URL:      mediaRoot + "/output.m3u8",
				Data:     string(output),
				Duration: duration,
			},
		},
		Subtitles:   "{}",
		UCP:         "a",
		AudioOnly:   true,
		AspectRatio: "0.00",
		PartySync:   true,
		LRCS:        "{}",
		Duration:    duration,
	})
}

func (server *FNRadioServer) createLiveBlurl(station *Station, c *gin.Context) ([]byte, error) {
	liveStation := server.LiveStations.GetOrCreate(station)

	master, err := os.ReadFile("media/" + liveStation.Folder + "/master.m3u8")
	if err != nil {
		return nil, err
	}

	mediaRoot := c.Request.Header.Get("X-API-Root") + "/media/" + url.PathEscape(liveStation.Folder)

	return encodeBlurl(&BLURL{
		Playlists: []Playlist{
			{
				Type:     "master",
				Language: "en",
				URL:      mediaRoot + "/master.m3u8",
				Data:     string(master),
			},
		},
		Subtitles:   "{}",
		UCP:         "a",
		AudioOnly:   true,
		AspectRatio: "0.00",
		PartySync:   false,
		LRCS:        "{}",
	})
}

func (server *FNRadioServer) cleanupBrokenStations() {
	dir, err := os.ReadDir("media")
	if err != nil {
		return
	}

	for _, file := range dir {
		if _, err := os.Stat("media/" + file.Name() + "/master.m3u8"); os.IsNotExist(err) {
			_ = os.RemoveAll("media/" + file.Name())
		}
	}
}

func (server *FNRadioServer) cleanupLiveStations() {
	for _, station := range server.LiveStations.Stations {
		station.Quit <- struct{}{}
	}

	dir, err := os.ReadDir("media")
	if err != nil {
		return
	}

	for _, file := range dir {
		if strings.HasPrefix(file.Name(), "LIVE_") {
			_ = os.RemoveAll("media/" + file.Name())
		}
	}
}
