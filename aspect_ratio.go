package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"math"
)

type AspectRatio struct {
	Streams []struct {
		Width float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"streams"`
}

func getVideoAspectRatio(filePath string)(string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var b bytes.Buffer
	cmd.Stdout = &b
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	aspectRatio := AspectRatio{}
	err = json.Unmarshal(b.Bytes(), &aspectRatio)
	if err != nil {
		return "", err
	}
	if len(aspectRatio.Streams) < 1 {
		return "", fmt.Errorf("error getting aspect ratio")
	}
	if math.Abs((aspectRatio.Streams[0].Width / aspectRatio.Streams[0].Height) - (16.0/9.0)) < 0.01 {
		return "16:9", nil
	}
	if math.Abs((aspectRatio.Streams[0].Width / aspectRatio.Streams[0].Height) - (9.0/16.0)) < 0.01 {
		return "9:16", nil
	}
	return "other", nil
}