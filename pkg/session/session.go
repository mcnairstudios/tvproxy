package session

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

const (
	ConsumerViewer    = "viewer"
	ConsumerRecording = "recording"
)

type Session struct {
	ID              string
	ChannelID       string
	StreamID        string
	StreamURL       string
	StreamName      string
	ChannelName     string
	ProfileName     string
	OutputVideoCodec string
	OutputAudioCodec string
	OutputContainer  string
	OutputHWAccel    string
	Delivery         string
	UseWireGuard     bool
	FilePath        string
	TempDir         string
	BufferedSecs    float64
	Duration        float64
	SeekOffset      float64
	Video           *media.VideoInfo
	AudioTracks     []media.AudioTrack
	OutputDir    string
	Recorded     bool
	SessionWatcher *Watcher
	seekGen      atomic.Int64
	startOpts    StartOpts
	consumers    map[string]*Consumer
	wasRecording bool
	cancel       func()
	stopPipeline func()
	seekFunc     func(float64)
	seekVersion  atomic.Uint64
	done         chan struct{}
	doneOnce     sync.Once
	err          error
	lastStderr   string
	lingerTimer  *time.Timer
	mu           sync.RWMutex
}

type Consumer struct {
	ID        string
	Type      string
	CreatedAt time.Time
}

func (s *Session) addConsumer(c *Consumer) {
	s.mu.Lock()
	s.consumers[c.ID] = c
	if c.Type == ConsumerRecording {
		s.wasRecording = true
	}
	s.mu.Unlock()
}

func (s *Session) removeConsumer(id string) int {
	s.mu.Lock()
	delete(s.consumers, id)
	count := len(s.consumers)
	s.mu.Unlock()
	return count
}

func (s *Session) consumerCount() int {
	s.mu.RLock()
	count := len(s.consumers)
	s.mu.RUnlock()
	return count
}

func (s *Session) ConsumerCount() int {
	return s.consumerCount()
}

func (s *Session) setBuffered(secs float64) {
	s.mu.Lock()
	s.BufferedSecs = secs
	s.mu.Unlock()
}

func (s *Session) getBuffered() float64 {
	s.mu.RLock()
	secs := s.BufferedSecs
	s.mu.RUnlock()
	return secs
}

func (s *Session) setError(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func (s *Session) getError() error {
	s.mu.RLock()
	err := s.err
	s.mu.RUnlock()
	return err
}

func (s *Session) GetError() string {
	if err := s.getError(); err != nil {
		return err.Error()
	}
	return ""
}

func (s *Session) setLastStderr(stderr string) {
	s.mu.Lock()
	s.lastStderr = stderr
	s.mu.Unlock()
}

func (s *Session) LastStderr() string {
	s.mu.RLock()
	v := s.lastStderr
	s.mu.RUnlock()
	return v
}

func (s *Session) SetStopPipeline(fn func()) {
	s.mu.Lock()
	s.stopPipeline = fn
	s.mu.Unlock()
}

func (s *Session) StopPipeline() {
	s.mu.Lock()
	fn := s.stopPipeline
	s.stopPipeline = nil
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (s *Session) SetSeekFunc(fn func(float64)) {
	s.mu.Lock()
	s.seekFunc = fn
	s.mu.Unlock()
}

func (s *Session) Seek(position float64) bool {
	s.mu.RLock()
	fn := s.seekFunc
	s.mu.RUnlock()
	if fn != nil {
		s.SeekOffset = position
		s.seekVersion.Add(1)
		fn(position)
		return true
	}
	return false
}

func (s *Session) SeekVersion() uint64 {
	return s.seekVersion.Load()
}

func (s *Session) SetProbeInfo(video *media.VideoInfo, audio []media.AudioTrack, duration float64) {
	s.mu.Lock()
	if video != nil {
		s.Video = video
	}
	if len(audio) > 0 {
		s.AudioTracks = audio
	}
	if duration > 0 {
		s.Duration = duration
	}
	s.mu.Unlock()
}

func (s *Session) GetProbeInfo() (*media.VideoInfo, []media.AudioTrack, float64) {
	s.mu.RLock()
	v := s.Video
	a := s.AudioTracks
	d := s.Duration
	s.mu.RUnlock()
	return v, a, d
}

func (s *Session) isDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Session) markDone() {
	s.doneOnce.Do(func() { close(s.done) })
}

func (s *Session) HasRecordingConsumer() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.consumers {
		if c.Type == ConsumerRecording {
			return true
		}
	}
	return false
}

func (s *Session) RecordingConsumerID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.consumers {
		if c.Type == ConsumerRecording {
			return c.ID
		}
	}
	return ""
}

func (s *Session) Record() {
	s.mu.Lock()
	s.Recorded = true
	s.mu.Unlock()
}

func (s *Session) IsRecorded() bool {
	s.mu.RLock()
	v := s.Recorded
	s.mu.RUnlock()
	return v
}

func (s *Session) SourceTSPath() string {
	return filepath.Join(s.OutputDir, "source.ts")
}

func (s *Session) ProbePBPath() string {
	return filepath.Join(s.OutputDir, "probe.pb")
}
