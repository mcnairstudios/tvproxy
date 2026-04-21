package session

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	tvproto "github.com/gavinmcnair/tvproxy/pkg/proto"
)

type Watcher struct {
	dir        string
	segDir     string
	generation atomic.Int64
	probe      atomic.Pointer[tvproto.Probe]
	signal     atomic.Pointer[tvproto.Signal]

	videoInit atomic.Pointer[[]byte]
	audioInit atomic.Pointer[[]byte]

	videoSegs segmentIndex
	audioSegs segmentIndex

	probeCh chan *tvproto.Probe

	watcher *fsnotify.Watcher
	log     zerolog.Logger
	done    chan struct{}
}

type segmentIndex struct {
	mu    sync.RWMutex
	files []string
	count int
}

func (si *segmentIndex) Add(path string) int {
	si.mu.Lock()
	si.files = append(si.files, path)
	sort.Strings(si.files)
	si.count = len(si.files)
	n := si.count
	si.mu.Unlock()
	return n
}

func (si *segmentIndex) Get(seq int) (string, bool) {
	si.mu.RLock()
	defer si.mu.RUnlock()
	if seq < 1 || seq > len(si.files) {
		return "", false
	}
	return si.files[seq-1], true
}

func (si *segmentIndex) Count() int {
	si.mu.RLock()
	n := si.count
	si.mu.RUnlock()
	return n
}

func (si *segmentIndex) Reset() {
	si.mu.Lock()
	si.files = nil
	si.count = 0
	si.mu.Unlock()
}

func NewWatcher(dir string, log zerolog.Logger) (*Watcher, error) {
	segDir := filepath.Join(dir, "segments")
	if err := os.MkdirAll(segDir, 0755); err != nil {
		return nil, err
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}
	if err := fsw.Add(segDir); err != nil {
		fsw.Close()
		return nil, err
	}

	w := &Watcher{
		dir:     dir,
		segDir:  segDir,
		probeCh: make(chan *tvproto.Probe, 1),
		watcher: fsw,
		log:     log.With().Str("component", "watcher").Str("dir", dir).Logger(),
		done:    make(chan struct{}),
	}
	w.generation.Store(1)

	w.scanExisting()
	go w.run()
	return w, nil
}

func (w *Watcher) scanExisting() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			w.handleFile(filepath.Join(w.dir, e.Name()))
		}
	}

	segEntries, err := os.ReadDir(w.segDir)
	if err != nil {
		return
	}
	for _, e := range segEntries {
		if !e.IsDir() {
			w.handleFile(filepath.Join(w.segDir, e.Name()))
		}
	}
}

func (w *Watcher) run() {
	defer close(w.done)
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
				continue
			}
			w.handleFile(event.Name)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.log.Warn().Err(err).Msg("fsnotify error")
		}
	}
}

func (w *Watcher) handleFile(path string) {
	name := filepath.Base(path)

	switch {
	case name == "probe.pb":
		w.loadProbe(path)
	case name == "signal.pb":
		w.loadSignal(path)
	case name == "init_video.mp4":
		w.loadInit(path, &w.videoInit, "video")
	case name == "init_audio.mp4":
		w.loadInit(path, &w.audioInit, "audio")
	case strings.HasPrefix(name, "video_") && strings.HasSuffix(name, ".m4s"):
		n := w.videoSegs.Add(path)
		w.log.Debug().Str("file", name).Int("count", n).Msg("video segment")
	case strings.HasPrefix(name, "audio_") && strings.HasSuffix(name, ".m4s"):
		n := w.audioSegs.Add(path)
		w.log.Debug().Str("file", name).Int("count", n).Msg("audio segment")
	}
}

func (w *Watcher) loadProbe(path string) {
	data, err := readFileRetry(path, 3, 50*time.Millisecond)
	if err != nil {
		w.log.Warn().Err(err).Msg("failed to read probe.pb")
		return
	}
	pb := &tvproto.Probe{}
	if err := proto.Unmarshal(data, pb); err != nil {
		w.log.Warn().Err(err).Msg("failed to unmarshal probe.pb")
		return
	}
	w.probe.Store(pb)
	w.log.Info().
		Str("video_codec", pb.VideoCodec).
		Str("codec_string", pb.VideoCodecString).
		Int32("width", pb.VideoWidth).
		Int32("height", pb.VideoHeight).
		Str("audio_src", pb.AudioSourceCodec).
		Int32("audio_src_ch", pb.AudioSourceChannels).
		Str("audio_out", pb.AudioOutputCodec).
		Int32("audio_out_ch", pb.AudioOutputChannels).
		Msg("probe.pb loaded")

	select {
	case w.probeCh <- pb:
	default:
	}
}

func (w *Watcher) loadSignal(path string) {
	data, err := readFileRetry(path, 3, 50*time.Millisecond)
	if err != nil {
		return
	}
	pb := &tvproto.Signal{}
	if err := proto.Unmarshal(data, pb); err != nil {
		return
	}
	w.signal.Store(pb)
}

func (w *Watcher) loadInit(path string, dest *atomic.Pointer[[]byte], track string) {
	data, err := readFileRetry(path, 3, 50*time.Millisecond)
	if err != nil {
		w.log.Warn().Err(err).Str("track", track).Msg("failed to read init segment")
		return
	}
	dest.Store(&data)
	w.log.Info().Str("track", track).Int("bytes", len(data)).Msg("init segment loaded")
}

func readFileRetry(path string, retries int, delay time.Duration) ([]byte, error) {
	var data []byte
	var err error
	for i := 0; i < retries; i++ {
		data, err = os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data, nil
		}
		time.Sleep(delay)
	}
	return data, err
}

func (w *Watcher) Probe() *tvproto.Probe {
	return w.probe.Load()
}

func (w *Watcher) Signal() *tvproto.Signal {
	return w.signal.Load()
}

func (w *Watcher) WaitProbe() <-chan *tvproto.Probe {
	return w.probeCh
}

func (w *Watcher) VideoInit() []byte {
	p := w.videoInit.Load()
	if p == nil {
		return nil
	}
	return *p
}

func (w *Watcher) AudioInit() []byte {
	p := w.audioInit.Load()
	if p == nil {
		return nil
	}
	return *p
}

func (w *Watcher) VideoSegment(seq int) ([]byte, bool) {
	return w.readSegment(&w.videoSegs, seq)
}

func (w *Watcher) AudioSegment(seq int) ([]byte, bool) {
	return w.readSegment(&w.audioSegs, seq)
}

func (w *Watcher) readSegment(idx *segmentIndex, seq int) ([]byte, bool) {
	path, ok := idx.Get(seq)
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

func (w *Watcher) VideoSegmentCount() int {
	return w.videoSegs.Count()
}

func (w *Watcher) AudioSegmentCount() int {
	return w.audioSegs.Count()
}

func (w *Watcher) Generation() int64 {
	return w.generation.Load()
}

func (w *Watcher) Reset() {
	w.generation.Add(1)
	w.probe.Store(nil)
	w.signal.Store(nil)
	w.videoInit.Store(nil)
	w.audioInit.Store(nil)
	w.videoSegs.Reset()
	w.audioSegs.Reset()
}

func (w *Watcher) Close() error {
	err := w.watcher.Close()
	<-w.done
	return err
}

func SegmentSeqFromFilename(name string) int {
	base := strings.TrimSuffix(name, ".m4s")
	parts := strings.Split(base, "_")
	if len(parts) < 2 {
		return 0
	}
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return n
}
