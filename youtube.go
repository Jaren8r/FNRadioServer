package main

import (
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/kkdai/youtube/v2"
)

var youtubeIDRegex = regexp.MustCompile(`^[A-Za-z0-9_\-]{11}$`)

func pickBestFormat(list youtube.FormatList) *youtube.Format {
	best := list[0]

	for _, format := range list {
		if format.AudioSampleRate > best.AudioSampleRate {
			best = format
		}
	}

	return &best
}

func extractYouTubeID(input string) (string, error) {
	parsed, err := url.Parse(input)
	if err != nil {
		return "", err
	}

	if parsed.Host == "www.youtube.com" && parsed.Path == "/watch" {
		v := parsed.Query().Get("v")

		if youtubeIDRegex.MatchString(v) {
			return v, nil
		}
	}

	if parsed.Host == "youtu.be" {
		v := parsed.Path[1:]

		if youtubeIDRegex.MatchString(v) {
			return v, nil
		}
	}

	return "", errors.New("invalid url")
}

func extractYouTubePlaylistID(input string) (string, error) {
	parsed, err := url.Parse(input)
	if err == nil {
		if parsed.Host == "www.youtube.com" && parsed.Path == "/playlist" && parsed.Query().Has("list") {
			return parsed.Query().Get("list"), nil
		}
	}

	return "", errors.New("invalid url")
}

func startYouTubeDownload(id string) error {
	client := youtube.Client{}

	video, err := client.GetVideo(id)
	if err != nil {
		return err
	}

	if video.Duration == 0 {
		return errors.New("live streams aren't supported")
	}

	if video.Duration > 1*time.Hour {
		return errors.New("videos longer than 1 hour aren't supported")
	}

	stream, _, err := client.GetStream(video, pickBestFormat(video.Formats))
	if err != nil {
		return err
	}

	go downloadYouTubeVideo(id, stream)

	return nil
}

func downloadYouTubeVideo(id string, stream io.ReadCloser) {
	dir := "media/YT_" + id

	command := exec.Command("ffmpeg", "-i", "-", "-vn", "-hls_playlist_type", "vod", "-hls_time", "2", "-hls_segment_type", "fmp4", "-hls_flags", "discont_start", "-c:a", "libfdk_aac", "-b:a", "192k", "-master_pl_name", "master.m3u8", dir+"/output.m3u8")

	pipe, err := command.StdinPipe()
	if err != nil {
		return
	}

	err = os.Mkdir(dir, 0755)
	if err != nil {
		return
	}

	go func() {
		_, _ = io.Copy(pipe, stream)

		_ = pipe.Close()
	}()

	err = command.Run()

	if err != nil {
		_ = os.Remove("media/YT_" + id)
		return
	}
}
