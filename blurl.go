package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
)

type Playlist struct {
	Type     string `json:"type"`
	Language string `json:"language,omitempty"`
	URL      string `json:"url"`
	Data     string `json:"data,omitempty"`
	Duration int    `json:"duration,omitempty"`
}

type BLURL struct {
	Playlists   []Playlist `json:"playlists"`
	Subtitles   string     `json:"subtitles,omitempty"`
	UCP         string     `json:"ucp,omitempty"`
	AudioOnly   bool       `json:"audioonly,omitempty"`
	AspectRatio string     `json:"aspectratio,omitempty"`
	PartySync   bool       `json:"partysync"`
	LRCS        string     `json:"lrcs,omitempty"`
	Duration    int        `json:"duration,omitempty"`
}

func encodeBlurl(data *BLURL) ([]byte, error) {
	marshaled, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return encodeBlurlData(marshaled)
}

func encodeBlurlData(data []byte) ([]byte, error) {
	var b bytes.Buffer

	writer := zlib.NewWriter(&b)

	_, err := writer.Write(data)
	if err != nil {
		return nil, err
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	deflated := b.Bytes()

	w := bytes.NewBuffer([]byte{})

	w.WriteString("blul")
	_ = binary.Write(w, binary.BigEndian, uint32(len(data)))
	w.Write(deflated)

	return w.Bytes(), nil
}
