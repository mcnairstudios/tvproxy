package dash

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type MP4Segmenter struct {
	index     *MP4Index
	filePath  string
	duration  float64
	startTime time.Time
	done      chan struct{}
	doneOnce  sync.Once
	ready     chan struct{}
	readyOnce sync.Once
	log       zerolog.Logger
}

func NewMP4Segmenter(filePath string, duration float64, log zerolog.Logger) *MP4Segmenter {
	return &MP4Segmenter{
		filePath:  filePath,
		duration:  duration,
		startTime: time.Now().UTC(),
		done:      make(chan struct{}),
		ready:     make(chan struct{}),
		log:       log.With().Str("component", "mp4_segmenter").Logger(),
	}
}

func (s *MP4Segmenter) Start(ctx context.Context) error {
	waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer waitCancel()
	for {
		info, err := os.Stat(s.filePath)
		if err == nil && info.Size() > 4096 {
			break
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("input file not ready: %w", waitCtx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}

	idx := NewMP4Index(s.filePath, s.log)
	if err := idx.Start(); err != nil {
		return err
	}
	s.index = idx

	go s.waitForFragments()

	return nil
}

func (s *MP4Segmenter) waitForFragments() {
	for {
		select {
		case <-s.done:
			return
		default:
			if s.index.FragmentCount() > 0 {
				s.readyOnce.Do(func() {
					s.log.Info().Int("fragments", s.index.FragmentCount()).Msg("mp4 segmenter ready")
					close(s.ready)
				})
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func (s *MP4Segmenter) WaitReady(ctx context.Context) error {
	select {
	case <-s.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return fmt.Errorf("segmenter stopped before ready")
	}
}

func (s *MP4Segmenter) Stop() {
	s.doneOnce.Do(func() {
		close(s.done)
	})
	if s.index != nil {
		s.index.MarkDone()
		s.index.Stop()
	}
}

func (s *MP4Segmenter) IsDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *MP4Segmenter) ServeVideoInit() ([]byte, error) {
	data := s.index.VideoInitData()
	if data == nil {
		return nil, fmt.Errorf("video init segment not available")
	}
	return data, nil
}

func (s *MP4Segmenter) ServeAudioInit() ([]byte, error) {
	data := s.index.AudioInitData()
	if data == nil {
		return nil, fmt.Errorf("audio init segment not available")
	}
	return data, nil
}

func (s *MP4Segmenter) readRawSegment(number int) ([]byte, error) {
	frag, ok := s.index.Fragment(number)
	if !ok {
		return nil, fmt.Errorf("segment %d not found", number)
	}

	needEnd := frag.FileOffset + frag.Size

	for attempts := 0; attempts < 10; attempts++ {
		f, err := os.Open(s.filePath)
		if err != nil {
			return nil, fmt.Errorf("opening file: %w", err)
		}

		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("stat file: %w", err)
		}

		if info.Size() >= needEnd {
			if _, err := f.Seek(frag.FileOffset, io.SeekStart); err != nil {
				f.Close()
				return nil, fmt.Errorf("seeking to segment: %w", err)
			}
			data := make([]byte, frag.Size)
			if _, err := io.ReadFull(f, data); err != nil {
				f.Close()
				return nil, fmt.Errorf("reading segment: %w", err)
			}
			f.Close()

			if len(data) >= 8 && string(data[4:8]) == "moof" {
				return data, nil
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		f.Close()
		time.Sleep(500 * time.Millisecond)
	}

	return nil, fmt.Errorf("segment %d not fully written after timeout", number)
}

func (s *MP4Segmenter) ServeVideoSegment(number int) ([]byte, error) {
	raw, err := s.readRawSegment(number)
	if err != nil {
		return nil, err
	}
	parsedInit := s.index.ParsedInit()
	if parsedInit == nil {
		return nil, fmt.Errorf("parsed init not available")
	}
	return demuxSegment(raw, parsedInit, s.index.VideoTrackID())
}

func (s *MP4Segmenter) ServeAudioSegment(number int) ([]byte, error) {
	raw, err := s.readRawSegment(number)
	if err != nil {
		return nil, err
	}
	parsedInit := s.index.ParsedInit()
	if parsedInit == nil {
		return nil, fmt.Errorf("parsed init not available")
	}
	return demuxSegment(raw, parsedInit, s.index.AudioTrackID())
}

func (s *MP4Segmenter) GenerateManifest(duration float64, bufferedSecs float64) []byte {
	tracks := s.index.Tracks()
	frags := s.index.Fragments()

	isComplete := s.index.IsDone() && duration > 0
	isDynamic := !isComplete

	videoTimescale := s.index.VideoTimescale()
	if videoTimescale == 0 {
		videoTimescale = 90000
	}
	audioTimescale := s.index.AudioTimescale()
	if audioTimescale == 0 {
		audioTimescale = 48000
	}

	hasVideo := s.index.VideoTrackID() > 0
	hasAudio := s.index.AudioTrackID() > 0

	var videoCodec, audioCodec string
	for _, t := range tracks {
		if t.HandlerType == "vide" && videoCodec == "" {
			videoCodec = fullCodecString(t.Codec)
		}
		if t.HandlerType == "soun" && audioCodec == "" {
			audioCodec = fullCodecString(t.Codec)
		}
	}
	if videoCodec == "" {
		videoCodec = "avc1.640028"
	}
	if audioCodec == "" {
		audioCodec = "mp4a.40.2"
	}

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")

	if isDynamic {
		ast := s.startTime
		if bufferedSecs > 0 {
			ast = time.Now().UTC().Add(-time.Duration(bufferedSecs+30) * time.Second)
		}
		buf.WriteString(fmt.Sprintf(`<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="dynamic" minimumUpdatePeriod="PT5S" availabilityStartTime="%s"`,
			ast.Format(time.RFC3339)))
		if duration > 0 {
			buf.WriteString(fmt.Sprintf(` mediaPresentationDuration="%s"`, formatISODur(duration)))
		}
		buf.WriteString(` minBufferTime="PT4S" timeShiftBufferDepth="PT5M" maxSegmentDuration="PT10S" profiles="urn:mpeg:dash:profile:isoff-live:2011">` + "\n")
	} else {
		buf.WriteString(fmt.Sprintf(`<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="%s" minBufferTime="PT2S" profiles="urn:mpeg:dash:profile:isoff-on-demand:2011">`+"\n",
			formatISODur(duration)))
	}

	buf.WriteString(`  <Period id="0" start="PT0S">` + "\n")

	if hasVideo {
		buf.WriteString(`    <AdaptationSet id="0" contentType="video" mimeType="video/mp4" segmentAlignment="true">` + "\n")
		buf.WriteString(fmt.Sprintf(`      <Representation id="0" codecs="%s" bandwidth="2000000">`+"\n", videoCodec))
		writeTimeline(&buf, videoTimescale, frags, false)
		buf.WriteString(`      </Representation>` + "\n")
		buf.WriteString(`    </AdaptationSet>` + "\n")
	}

	if hasAudio {
		buf.WriteString(`    <AdaptationSet id="1" contentType="audio" mimeType="audio/mp4" segmentAlignment="true">` + "\n")
		buf.WriteString(fmt.Sprintf(`      <Representation id="1" codecs="%s" bandwidth="128000" audioSamplingRate="%d">`+"\n", audioCodec, audioTimescale))
		writeTimeline(&buf, audioTimescale, frags, true)
		buf.WriteString(`      </Representation>` + "\n")
		buf.WriteString(`    </AdaptationSet>` + "\n")
	}

	buf.WriteString(`  </Period>` + "\n")
	buf.WriteString(`</MPD>` + "\n")

	return buf.Bytes()
}

func writeTimeline(buf *bytes.Buffer, timescale uint32, frags []FragmentEntry, isAudio bool) {
	initName := "init_v.mp4"
	mediaPattern := "seg_v_$Number$.m4s"
	if isAudio {
		initName = "init_a.mp4"
		mediaPattern = "seg_a_$Number$.m4s"
	}

	buf.WriteString(fmt.Sprintf(`        <SegmentTemplate timescale="%d" initialization="%s" media="%s" startNumber="0">`+"\n",
		timescale, initName, mediaPattern))
	buf.WriteString(`          <SegmentTimeline>` + "\n")

	type sEntry struct {
		t uint64
		d uint64
		r int
	}

	var entries []sEntry
	for _, f := range frags {
		dt := f.DecodeTime
		dur := f.Duration
		if isAudio {
			dt = f.AudioDecodeTime
			dur = f.AudioDuration
		}
		if dur == 0 {
			continue
		}
		if len(entries) > 0 {
			last := &entries[len(entries)-1]
			if dur == last.d {
				last.r++
				continue
			}
		}
		entries = append(entries, sEntry{t: dt, d: dur, r: 0})
	}

	for _, e := range entries {
		if e.r > 0 {
			buf.WriteString(fmt.Sprintf(`            <S t="%d" d="%d" r="%d"/>`+"\n", e.t, e.d, e.r))
		} else {
			buf.WriteString(fmt.Sprintf(`            <S t="%d" d="%d"/>`+"\n", e.t, e.d))
		}
	}

	buf.WriteString(`          </SegmentTimeline>` + "\n")
	buf.WriteString(`        </SegmentTemplate>` + "\n")
}

func fullCodecString(codec string) string {
	switch codec {
	case "avc1":
		return "avc1.640028"
	case "hev1":
		return "hev1.1.6.L120.90"
	case "av01":
		return "av01.0.08M.08"
	case "mp4a":
		return "mp4a.40.2"
	case "ac-3":
		return "ac-3"
	case "ec-3":
		return "ec-3"
	default:
		return codec
	}
}

func formatISODur(seconds float64) string {
	h := int(math.Floor(seconds / 3600))
	m := int(math.Floor(math.Mod(seconds, 3600) / 60))
	s := math.Mod(seconds, 60)
	var parts []string
	parts = append(parts, "PT")
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dH", h))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%dM", m))
	}
	parts = append(parts, fmt.Sprintf("%.1fS", s))
	return strings.Join(parts, "")
}
