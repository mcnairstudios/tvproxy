package dash

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/rs/zerolog"
)

type TrackInfo struct {
	TrackID     uint32
	Timescale   uint32
	HandlerType string
	Codec       string
}

type FragmentEntry struct {
	Number          int
	FileOffset      int64
	Size            int64
	DecodeTime      uint64
	Duration        uint64
	AudioDecodeTime uint64
	AudioDuration   uint64
}

type MP4Index struct {
	initSize       int64
	initData       []byte
	parsedInit     *mp4.InitSegment
	videoInitData  []byte
	audioInitData  []byte
	tracks         map[uint32]*TrackInfo
	videoTrackID   uint32
	audioTrackID   uint32
	videoTimescale uint32
	audioTimescale uint32
	fragments      []FragmentEntry
	fileOffset     int64
	filePath       string
	done           bool
	mu             sync.RWMutex
	stop           chan struct{}
	stopped        chan struct{}
	log            zerolog.Logger
}

func NewMP4Index(filePath string, log zerolog.Logger) *MP4Index {
	return &MP4Index{
		tracks:   make(map[uint32]*TrackInfo),
		filePath: filePath,
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
		log:      log.With().Str("component", "mp4_index").Logger(),
	}
}

func (idx *MP4Index) Start() error {
	if err := idx.parseInit(); err != nil {
		return fmt.Errorf("parsing init segment: %w", err)
	}
	go idx.pollLoop()
	return nil
}

func (idx *MP4Index) Stop() {
	close(idx.stop)
	<-idx.stopped
}

func (idx *MP4Index) parseInit() error {
	f, err := os.Open(idx.filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	parsed, err := mp4.DecodeFile(f, mp4.WithDecodeMode(mp4.DecModeLazyMdat), mp4.WithDecodeFlags(mp4.DecStartOnMoof))
	if err != nil && parsed == nil {
		return fmt.Errorf("decoding mp4 header: %w", err)
	}

	if parsed.Init == nil {
		return fmt.Errorf("no init segment found (ftyp+moov)")
	}

	initBuf := &bytesWriter{}
	if err := parsed.Init.Encode(initBuf); err != nil {
		return fmt.Errorf("encoding init segment: %w", err)
	}

	idx.mu.Lock()
	idx.initData = initBuf.Bytes()
	idx.parsedInit = parsed.Init
	idx.initSize = int64(len(idx.initData))
	idx.fileOffset = idx.initSize

	if parsed.Moov != nil {
		for _, trak := range parsed.Moov.Traks {
			ti := &TrackInfo{
				TrackID: trak.Tkhd.TrackID,
			}
			if trak.Mdia != nil {
				if trak.Mdia.Mdhd != nil {
					ti.Timescale = trak.Mdia.Mdhd.Timescale
				}
				if trak.Mdia.Hdlr != nil {
					ti.HandlerType = trak.Mdia.Hdlr.HandlerType
				}
				if trak.Mdia.Minf != nil && trak.Mdia.Minf.Stbl != nil && trak.Mdia.Minf.Stbl.Stsd != nil {
					ti.Codec = detectCodec(trak.Mdia.Minf.Stbl.Stsd)
				}
			}
			idx.tracks[ti.TrackID] = ti

			if ti.HandlerType == "vide" && idx.videoTrackID == 0 {
				idx.videoTrackID = ti.TrackID
				idx.videoTimescale = ti.Timescale
			}
			if ti.HandlerType == "soun" && idx.audioTrackID == 0 {
				idx.audioTrackID = ti.TrackID
				idx.audioTimescale = ti.Timescale
			}
		}
	}

	if idx.videoTrackID > 0 {
		if vInit, err := filterInitForTrack(idx.initData, idx.videoTrackID); err == nil {
			idx.videoInitData = vInit
		} else {
			idx.log.Warn().Err(err).Msg("failed to create video-only init")
		}
	}
	if idx.audioTrackID > 0 {
		if aInit, err := filterInitForTrack(idx.initData, idx.audioTrackID); err == nil {
			idx.audioInitData = aInit
		} else {
			idx.log.Warn().Err(err).Msg("failed to create audio-only init")
		}
	}

	for _, seg := range parsed.Segments {
		for _, frag := range seg.Fragments {
			idx.indexFragment(frag)
		}
	}

	idx.mu.Unlock()

	idx.log.Info().
		Int("tracks", len(idx.tracks)).
		Int64("init_size", idx.initSize).
		Int("fragments", len(idx.fragments)).
		Uint32("video_track", idx.videoTrackID).
		Uint32("audio_track", idx.audioTrackID).
		Uint32("video_timescale", idx.videoTimescale).
		Uint32("audio_timescale", idx.audioTimescale).
		Msg("mp4 index initialized")

	return nil
}

func (idx *MP4Index) indexFragment(frag *mp4.Fragment) {
	if frag.Moof == nil {
		return
	}

	fragOffset := int64(frag.StartPos)
	var fragSize int64
	for _, child := range frag.Children {
		fragSize += int64(child.Size())
	}

	entry := FragmentEntry{
		Number:     len(idx.fragments),
		FileOffset: fragOffset,
		Size:       fragSize,
	}

	for _, traf := range frag.Moof.Trafs {
		if traf.Tfhd == nil || traf.Tfdt == nil {
			continue
		}
		dur := trafDuration(traf)
		if traf.Tfhd.TrackID == idx.videoTrackID {
			entry.DecodeTime = traf.Tfdt.BaseMediaDecodeTime()
			entry.Duration = dur
		}
		if traf.Tfhd.TrackID == idx.audioTrackID {
			entry.AudioDecodeTime = traf.Tfdt.BaseMediaDecodeTime()
			entry.AudioDuration = dur
		}
	}

	if entry.Duration == 0 && entry.AudioDuration > 0 {
		entry.DecodeTime = entry.AudioDecodeTime
		entry.Duration = entry.AudioDuration
	}

	idx.fragments = append(idx.fragments, entry)

	endOffset := fragOffset + fragSize
	if endOffset > idx.fileOffset {
		idx.fileOffset = endOffset
	}
}

func trafDuration(traf *mp4.TrafBox) uint64 {
	var dur uint64
	for _, trun := range traf.Truns {
		for _, sample := range trun.Samples {
			if sample.Dur > 0 {
				dur += uint64(sample.Dur)
			} else if traf.Tfhd.HasDefaultSampleDuration() {
				dur += uint64(traf.Tfhd.DefaultSampleDuration)
			}
		}
	}
	return dur
}

func (idx *MP4Index) pollLoop() {
	defer close(idx.stopped)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-idx.stop:
			return
		case <-ticker.C:
			idx.scanNewFragments()
		}
	}
}

func (idx *MP4Index) scanNewFragments() {
	idx.mu.RLock()
	offset := idx.fileOffset
	idx.mu.RUnlock()

	f, err := os.Open(idx.filePath)
	if err != nil {
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.Size() <= offset {
		return
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return
	}

	var newFragments []FragmentEntry
	pos := offset

	for {
		hdr, err := mp4.DecodeHeader(f)
		if err != nil {
			break
		}

		boxEnd := pos + int64(hdr.Size)
		if boxEnd > info.Size() {
			break
		}

		if hdr.Name == "moof" {
			moofStart := pos
			moofSize := int64(hdr.Size)

			if _, err := f.Seek(moofStart, io.SeekStart); err != nil {
				break
			}

			box, err := mp4.DecodeBoxLazyMdat(uint64(moofStart), f)
			if err != nil {
				break
			}
			moofBox, ok := box.(*mp4.MoofBox)
			if !ok {
				pos = boxEnd
				continue
			}

			currentPos := moofStart + moofSize
			if _, err := f.Seek(currentPos, io.SeekStart); err != nil {
				break
			}

			mdatHdr, err := mp4.DecodeHeader(f)
			if err != nil {
				break
			}
			if mdatHdr.Name != "mdat" {
				pos = boxEnd
				continue
			}

			mdatEnd := currentPos + int64(mdatHdr.Size)
			if mdatEnd > info.Size() {
				break
			}

			fragSize := moofSize + int64(mdatHdr.Size)

			idx.mu.RLock()
			vidTrack := idx.videoTrackID
			audTrack := idx.audioTrackID
			idx.mu.RUnlock()

			entry := FragmentEntry{
				FileOffset: moofStart,
				Size:       fragSize,
			}

			for _, traf := range moofBox.Trafs {
				if traf.Tfhd == nil || traf.Tfdt == nil {
					continue
				}
				dur := trafDuration(traf)
				if vidTrack > 0 && traf.Tfhd.TrackID == vidTrack {
					entry.DecodeTime = traf.Tfdt.BaseMediaDecodeTime()
					entry.Duration = dur
				}
				if audTrack > 0 && traf.Tfhd.TrackID == audTrack {
					entry.AudioDecodeTime = traf.Tfdt.BaseMediaDecodeTime()
					entry.AudioDuration = dur
				}
			}

			if entry.Duration == 0 && entry.AudioDuration > 0 {
				entry.DecodeTime = entry.AudioDecodeTime
				entry.Duration = entry.AudioDuration
			}

			newFragments = append(newFragments, entry)

			pos = moofStart + fragSize
			if _, err := f.Seek(pos, io.SeekStart); err != nil {
				break
			}
		} else {
			if _, err := f.Seek(boxEnd, io.SeekStart); err != nil {
				break
			}
			pos = boxEnd
		}
	}

	if len(newFragments) > 0 {
		idx.mu.Lock()
		for i := range newFragments {
			newFragments[i].Number = len(idx.fragments)
			idx.fragments = append(idx.fragments, newFragments[i])
		}
		if pos > idx.fileOffset {
			idx.fileOffset = pos
		}
		idx.mu.Unlock()

		idx.log.Debug().Int("new_fragments", len(newFragments)).Int64("offset", pos).Msg("indexed new fragments")
	}
}

func (idx *MP4Index) InitData() []byte {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.initData
}

func (idx *MP4Index) Tracks() map[uint32]*TrackInfo {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	result := make(map[uint32]*TrackInfo, len(idx.tracks))
	for k, v := range idx.tracks {
		result[k] = v
	}
	return result
}

func (idx *MP4Index) Fragments() []FragmentEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	result := make([]FragmentEntry, len(idx.fragments))
	copy(result, idx.fragments)
	return result
}

func (idx *MP4Index) Fragment(number int) (FragmentEntry, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if number < 0 || number >= len(idx.fragments) {
		return FragmentEntry{}, false
	}
	return idx.fragments[number], true
}

func (idx *MP4Index) FragmentCount() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.fragments)
}

func (idx *MP4Index) VideoTimescale() uint32 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.videoTimescale
}

func (idx *MP4Index) VideoTrackID() uint32 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.videoTrackID
}

func (idx *MP4Index) AudioTrackID() uint32 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.audioTrackID
}

func (idx *MP4Index) AudioTimescale() uint32 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.audioTimescale
}

func (idx *MP4Index) VideoInitData() []byte {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.videoInitData
}

func (idx *MP4Index) AudioInitData() []byte {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.audioInitData
}

func (idx *MP4Index) ParsedInit() *mp4.InitSegment {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.parsedInit
}

func (idx *MP4Index) MarkDone() {
	idx.mu.Lock()
	idx.done = true
	idx.mu.Unlock()
	idx.scanNewFragments()
}

func (idx *MP4Index) IsDone() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.done
}

func detectCodec(stsd *mp4.StsdBox) string {
	if stsd == nil {
		return ""
	}
	if stsd.AvcX != nil {
		return "avc1"
	}
	if stsd.HvcX != nil {
		return "hev1"
	}
	if stsd.Av01 != nil {
		return "av01"
	}
	if stsd.Mp4a != nil {
		return "mp4a"
	}
	if stsd.AC3 != nil {
		return "ac-3"
	}
	if stsd.EC3 != nil {
		return "ec-3"
	}
	return ""
}

type bytesWriter struct {
	data []byte
}

func (w *bytesWriter) Write(p []byte) (int, error) {
	w.data = append(w.data, p...)
	return len(p), nil
}

func (w *bytesWriter) Bytes() []byte {
	return w.data
}
