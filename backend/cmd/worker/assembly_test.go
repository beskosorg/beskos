package main

import (
	"strings"
	"testing"
)

func TestXstackLayout(t *testing.T) {
	cases := map[int]string{
		2: "0_0|640_0",
		3: "0_0|640_0|0_360",
		4: "0_0|640_0|0_360|640_360",
		5: "0_0|640_0|1280_0|0_360|640_360",
	}
	for n, want := range cases {
		if got := xstackLayout(n); got != want {
			t.Errorf("xstackLayout(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFfmpegArgsShapes(t *testing.T) {
	// 2 videos + 2 audios with offset → xstack + amix, with tpad/adelay.
	args := strings.Join(ffmpegArgs([]trackInput{
		{path: "a.mp4", kind: "video", offset: 0},
		{path: "b.mp4", kind: "video", offset: 2.5},
		{path: "a.ogg", kind: "audio", offset: 0},
		{path: "b.ogg", kind: "audio", offset: 2.5},
	}, "out.mp4"), " ")
	for _, want := range []string{
		"xstack=inputs=2", "amix=inputs=2", "adelay=2500:all=1",
		"tpad=start_duration=2.500", "-c:v libx264", "-c:a aac", "+faststart",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q in: %s", want, args)
		}
	}

	// Audio only → no mapped video nor libx264.
	audioOnly := strings.Join(ffmpegArgs([]trackInput{{path: "a.ogg", kind: "audio"}}, "out.mp4"), " ")
	if strings.Contains(audioOnly, "libx264") || strings.Contains(audioOnly, "[vout]") {
		t.Errorf("audio-only should not map video: %s", audioOnly)
	}
	if !strings.Contains(audioOnly, "acopy[aout]") {
		t.Errorf("audio-only should copy the single audio: %s", audioOnly)
	}
}
