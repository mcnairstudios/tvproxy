package demuxloop

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demux"
)

// --- mock reader ---

type mockReader struct {
	packets []*demux.Packet
	idx     int
}

func (m *mockReader) ReadPacket() (*demux.Packet, error) {
	if m.idx >= len(m.packets) {
		return nil, io.EOF
	}
	pkt := m.packets[m.idx]
	m.idx++
	return pkt, nil
}

// blockingReader blocks until context is cancelled, useful for cancel tests.
type blockingReader struct {
	ctx     context.Context
	packets []*demux.Packet
	idx     int
}

func (b *blockingReader) ReadPacket() (*demux.Packet, error) {
	if b.idx >= len(b.packets) {
		// Block until context cancelled.
		<-b.ctx.Done()
		return nil, b.ctx.Err()
	}
	pkt := b.packets[b.idx]
	b.idx++
	return pkt, nil
}

// --- mock sink ---

type mockVideoPush struct {
	data     []byte
	pts, dts int64
	keyframe bool
}

type mockAudioPush struct {
	data     []byte
	pts, dts int64
}

type mockSubPush struct {
	data     []byte
	pts      int64
	duration int64
}

type mockSink struct {
	videos    []mockVideoPush
	audios    []mockAudioPush
	subtitles []mockSubPush
	eosCount  int
	pushErr   error // if set, all Push* methods return this
}

func (s *mockSink) PushVideo(data []byte, pts, dts int64, keyframe bool) error {
	if s.pushErr != nil {
		return s.pushErr
	}
	s.videos = append(s.videos, mockVideoPush{data: data, pts: pts, dts: dts, keyframe: keyframe})
	return nil
}

func (s *mockSink) PushAudio(data []byte, pts, dts int64) error {
	if s.pushErr != nil {
		return s.pushErr
	}
	s.audios = append(s.audios, mockAudioPush{data: data, pts: pts, dts: dts})
	return nil
}

func (s *mockSink) PushSubtitle(data []byte, pts int64, duration int64) error {
	if s.pushErr != nil {
		return s.pushErr
	}
	s.subtitles = append(s.subtitles, mockSubPush{data: data, pts: pts, duration: duration})
	return nil
}

func (s *mockSink) EndOfStream() {
	s.eosCount++
}

// --- tests ---

func TestRun_EOF(t *testing.T) {
	reader := &mockReader{
		packets: []*demux.Packet{
			{Type: demux.Video, Data: []byte{1}, PTS: 100, DTS: 90, Keyframe: true},
			{Type: demux.Audio, Data: []byte{2}, PTS: 200, DTS: 200},
			{Type: demux.Video, Data: []byte{3}, PTS: 300, DTS: 290, Keyframe: false},
		},
	}
	sink := &mockSink{}

	err := Run(context.Background(), Config{Reader: reader, Sink: sink})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if len(sink.videos) != 2 {
		t.Fatalf("expected 2 video pushes, got %d", len(sink.videos))
	}
	if len(sink.audios) != 1 {
		t.Fatalf("expected 1 audio push, got %d", len(sink.audios))
	}
	if sink.eosCount != 1 {
		t.Fatalf("expected EndOfStream called once, got %d", sink.eosCount)
	}

	// Verify first video packet fields.
	v := sink.videos[0]
	if v.pts != 100 || v.dts != 90 || !v.keyframe {
		t.Errorf("video[0] = %+v, unexpected", v)
	}
	// Verify second video is not keyframe.
	if sink.videos[1].keyframe {
		t.Error("video[1] should not be keyframe")
	}
	// Verify audio packet.
	a := sink.audios[0]
	if a.pts != 200 || a.dts != 200 {
		t.Errorf("audio[0] = %+v, unexpected", a)
	}
}

func TestRun_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	reader := &blockingReader{
		ctx: ctx,
		packets: []*demux.Packet{
			{Type: demux.Video, Data: []byte{1}, PTS: 100, DTS: 90},
		},
	}
	sink := &mockSink{}

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{Reader: reader, Sink: sink})
	}()

	// Cancel after the first packet is read but before the second read completes.
	cancel()

	err := <-done
	if err != nil {
		t.Fatalf("expected nil on cancel, got %v", err)
	}
	if sink.eosCount != 0 {
		t.Error("EndOfStream should not be called on cancel")
	}
}

func TestRun_SinkError(t *testing.T) {
	sinkErr := errors.New("appsrc full")
	reader := &mockReader{
		packets: []*demux.Packet{
			{Type: demux.Video, Data: []byte{1}, PTS: 100, DTS: 90},
		},
	}
	sink := &mockSink{pushErr: sinkErr}

	err := Run(context.Background(), Config{Reader: reader, Sink: sink})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sinkErr) {
		t.Fatalf("expected wrapped sinkErr, got %v", err)
	}
	if sink.eosCount != 0 {
		t.Error("EndOfStream should not be called on error")
	}
}

func TestRun_EmptyStream(t *testing.T) {
	reader := &mockReader{packets: nil}
	sink := &mockSink{}

	err := Run(context.Background(), Config{Reader: reader, Sink: sink})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if sink.eosCount != 1 {
		t.Fatalf("expected EndOfStream called once, got %d", sink.eosCount)
	}
}

func TestRun_MixedPackets(t *testing.T) {
	reader := &mockReader{
		packets: []*demux.Packet{
			{Type: demux.Video, Data: []byte{0x01}, PTS: 1000, DTS: 900, Keyframe: true},
			{Type: demux.Audio, Data: []byte{0x02}, PTS: 1100, DTS: 1100},
			{Type: demux.Subtitle, Data: []byte{0x03}, PTS: 1200, Duration: 5000},
			{Type: demux.Audio, Data: []byte{0x04}, PTS: 2100, DTS: 2100},
			{Type: demux.Video, Data: []byte{0x05}, PTS: 2000, DTS: 1900, Keyframe: false},
		},
	}
	sink := &mockSink{}

	err := Run(context.Background(), Config{Reader: reader, Sink: sink})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	if len(sink.videos) != 2 {
		t.Fatalf("expected 2 videos, got %d", len(sink.videos))
	}
	if len(sink.audios) != 2 {
		t.Fatalf("expected 2 audios, got %d", len(sink.audios))
	}
	if len(sink.subtitles) != 1 {
		t.Fatalf("expected 1 subtitle, got %d", len(sink.subtitles))
	}

	sub := sink.subtitles[0]
	if sub.pts != 1200 || sub.duration != 5000 {
		t.Errorf("subtitle = %+v, unexpected", sub)
	}
	if sink.eosCount != 1 {
		t.Fatalf("expected 1 EOS, got %d", sink.eosCount)
	}
}

func TestRun_ReaderError(t *testing.T) {
	readErr := errors.New("network failure")

	reader := &errorReader{err: readErr}
	sink := &mockSink{}

	err := Run(context.Background(), Config{Reader: reader, Sink: sink})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, readErr) {
		t.Fatalf("expected wrapped readErr, got %v", err)
	}
	if sink.eosCount != 0 {
		t.Error("EndOfStream should not be called on read error")
	}
}

// errorReader always returns an error.
type errorReader struct {
	err error
}

func (e *errorReader) ReadPacket() (*demux.Packet, error) {
	return nil, e.err
}
