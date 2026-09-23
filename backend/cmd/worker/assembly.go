package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

// OFFLINE recording assembly: LiveKit records one raw track per participant (track egress);
// here ffmpeg composes the final MP4 (video grid + audio mix, offset by each track's start time)
// outside the meeting's hot path. Jobs are claimed by priority in ClaimAssemblyJob, so
// offline_priority plans assemble first.

// startAssembly starts the queue consumer (30s ticker; each tick drains the ready jobs).
func startAssembly(q *db.Queries, st *storage.Service) {
	if st == nil {
		log.Printf("assembly worker disabled (no storage)")
		return
	}
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			for {
				rec, err := q.ClaimAssemblyJob(context.Background())
				if err != nil {
					break // empty queue
				}
				assembleRecording(context.Background(), q, st, rec)
			}
		}
	}()
	log.Printf("assembly worker started (offline recording composition)")
}

// assembleRecording downloads the tracks, composes them with ffmpeg and uploads the final MP4.
func assembleRecording(ctx context.Context, q *db.Queries, st *storage.Service, rec db.Recording) {
	start := time.Now()
	tracks, err := q.ListRecordingTracks(ctx, rec.ID)
	if err != nil {
		log.Printf("assembly %s: list tracks: %v", rec.ID, err)
		_ = q.SetRecordingAssemblyFailed(ctx, rec.ID)
		return
	}
	tmp, err := os.MkdirTemp("", "assembly-*")
	if err != nil {
		_ = q.SetRecordingAssemblyFailed(ctx, rec.ID)
		return
	}
	defer os.RemoveAll(tmp)

	// Downloads the completed tracks; offset = track start − recording start (alignment).
	var inputs []trackInput
	var rawKeys []string
	for _, t := range tracks {
		if t.Status != "done" || t.BlobKey == "" {
			continue
		}
		local := filepath.Join(tmp, fmt.Sprintf("in-%d%s", len(inputs), filepath.Ext(t.BlobKey)))
		if err := downloadBlob(ctx, st, t.BlobKey, local); err != nil {
			log.Printf("assembly %s: download %s: %v", rec.ID, t.BlobKey, err)
			continue
		}
		off := t.StartedAt.Sub(rec.StartedAt).Seconds()
		if off < 0 {
			off = 0
		}
		inputs = append(inputs, trackInput{path: local, kind: t.Kind, offset: off})
		rawKeys = append(rawKeys, t.BlobKey)
	}
	if len(inputs) == 0 {
		log.Printf("assembly %s: no usable tracks", rec.ID)
		_ = q.SetRecordingAssemblyFailed(ctx, rec.ID)
		return
	}

	out := filepath.Join(tmp, "out.mp4")
	args := ffmpegArgs(inputs, out)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if outB, err := cmd.CombinedOutput(); err != nil {
		log.Printf("assembly %s: ffmpeg: %v: %s", rec.ID, err, tail(string(outB), 800))
		_ = q.SetRecordingAssemblyFailed(ctx, rec.ID)
		return
	}

	// Publishes the final MP4 and closes the recording.
	//
	// Tenant prefix: this is what makes the recording get encrypted with THAT tenant's key
	// instead of the KEK, and therefore fall within the per-tenant deletion guarantee — deleting
	// a customer's key must make everything of theirs unreadable, recordings included. Old
	// recordings, under `recordings/…`, still read fine: their key is stored in the database.
	key := fmt.Sprintf("%s/recordings/%s.mp4", rec.TenantID, rec.ID)
	f, err := os.Open(out)
	if err != nil {
		_ = q.SetRecordingAssemblyFailed(ctx, rec.ID)
		return
	}
	defer f.Close()
	info, _ := f.Stat()
	if err := st.Put(ctx, key, f, info.Size(), "video/mp4"); err != nil {
		log.Printf("assembly %s: upload: %v", rec.ID, err)
		_ = q.SetRecordingAssemblyFailed(ctx, rec.ID)
		return
	}
	size := info.Size()
	dur := int32(probeDuration(ctx, out))
	if err := q.SetRecordingAssembled(ctx, db.SetRecordingAssembledParams{
		ID: rec.ID, BlobKey: key, DurationSeconds: &dur, SizeBytes: &size,
	}); err != nil {
		log.Printf("assembly %s: persist: %v", rec.ID, err)
		return
	}
	// Success: the raw tracks are no longer needed (storage savings).
	for _, k := range rawKeys {
		_ = st.Remove(ctx, k)
	}
	log.Printf("assembly %s: ready (%d tracks, %ds, %d bytes, took %s)",
		rec.ID, len(inputs), dur, size, time.Since(start).Round(time.Second))
}

// trackInput is a downloaded track, ready for ffmpeg.
type trackInput struct {
	path   string
	kind   string
	offset float64 // seconds since the start of the recording
}

// ffmpegArgs builds the composition: videos in a grid (xstack, 640×360 cells, delayed via tpad)
// and audios in amix (delayed via adelay). No video → audio-only MP4 (<video> still plays it).
func ffmpegArgs(inputs []trackInput, out string) []string {
	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	for _, in := range inputs {
		args = append(args, "-i", in.path)
	}
	var filters, alabels, vlabels []string
	for i, in := range inputs {
		switch in.kind {
		case "audio":
			ms := int(in.offset * 1000)
			filters = append(filters, fmt.Sprintf("[%d:a]adelay=%d:all=1[a%d]", i, ms, i))
			alabels = append(alabels, fmt.Sprintf("[a%d]", i))
		case "video":
			filters = append(filters, fmt.Sprintf(
				"[%d:v]scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2,"+
					"tpad=start_duration=%.3f:color=black,setsar=1[v%d]", i, in.offset, i))
			vlabels = append(vlabels, fmt.Sprintf("[v%d]", i))
		}
	}
	maps := []string{}
	if len(vlabels) == 1 {
		filters = append(filters, vlabels[0]+"copy[vout]")
		maps = append(maps, "-map", "[vout]")
	} else if len(vlabels) > 1 {
		filters = append(filters, strings.Join(vlabels, "")+
			fmt.Sprintf("xstack=inputs=%d:layout=%s:fill=black[vout]", len(vlabels), xstackLayout(len(vlabels))))
		maps = append(maps, "-map", "[vout]")
	}
	if len(alabels) == 1 {
		filters = append(filters, alabels[0]+"acopy[aout]")
		maps = append(maps, "-map", "[aout]")
	} else if len(alabels) > 1 {
		filters = append(filters, strings.Join(alabels, "")+
			fmt.Sprintf("amix=inputs=%d:duration=longest:normalize=0[aout]", len(alabels)))
		maps = append(maps, "-map", "[aout]")
	}
	args = append(args, "-filter_complex", strings.Join(filters, ";"))
	args = append(args, maps...)
	if len(vlabels) > 0 {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "26")
	}
	if len(alabels) > 0 {
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}
	return append(args, "-movflags", "+faststart", out)
}

// xstackLayout returns the grid positions (columns = ⌈√n⌉, 640×360 cells).
func xstackLayout(n int) string {
	cols := 1
	for cols*cols < n {
		cols++
	}
	pos := make([]string, n)
	for i := 0; i < n; i++ {
		pos[i] = fmt.Sprintf("%d_%d", (i%cols)*640, (i/cols)*360)
	}
	return strings.Join(pos, "|")
}

func downloadBlob(ctx context.Context, st *storage.Service, key, dest string) error {
	obj, err := st.Get(ctx, key)
	if err != nil {
		return err
	}
	defer obj.Close()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, obj)
	return err
}

// probeDuration reads the final file's duration (s) via ffprobe; 0 on error (non-blocking).
func probeDuration(ctx context.Context, path string) float64 {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return v
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
