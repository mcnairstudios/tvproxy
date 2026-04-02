package session

import (
	"sync"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
)

const (
	ConsumerViewer    = "viewer"
	ConsumerRecording = "recording"
)

type Session struct {
	ID           string
	ChannelID    string
	StreamID     string
	StreamURL    string
	StreamName   string
	ChannelName  string
	ProfileName  string
	FilePath     string
	TempDir      string
	BufferedSecs float64
	Duration     float64
	Video        *ffmpeg.VideoInfo
	AudioTracks  []ffmpeg.AudioTrack
	consumers    map[string]*Consumer
	cancel       func()
	done         chan struct{}
	doneOnce     sync.Once
	err          error
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

func (s *Session) SetProbeInfo(video *ffmpeg.VideoInfo, audio []ffmpeg.AudioTrack, duration float64) {
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

func (s *Session) GetProbeInfo() (*ffmpeg.VideoInfo, []ffmpeg.AudioTrack, float64) {
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
