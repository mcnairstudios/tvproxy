// Package demuxloop provides the main demux goroutine pattern that reads
// packets from a media source and pushes them to a consumer (e.g. GStreamer
// appsrc). It handles context cancellation and EOS signaling.
package demuxloop

import (
	"context"
	"fmt"
	"io"

	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demux"
)

// PacketReader reads the next packet from a media source.
// *demux.Demuxer satisfies this interface.
type PacketReader interface {
	ReadPacket() (*demux.Packet, error)
}

// PacketSink receives packets from the demux loop.
// Implementations push packets to GStreamer appsrc or other consumers.
type PacketSink interface {
	// PushVideo pushes a video packet. Data is compressed (e.g. H264 NALUs).
	PushVideo(data []byte, pts, dts int64, keyframe bool) error
	// PushAudio pushes an audio packet.
	PushAudio(data []byte, pts, dts int64) error
	// PushSubtitle pushes a subtitle packet.
	PushSubtitle(data []byte, pts int64, duration int64) error
	// EndOfStream signals that no more packets will arrive.
	EndOfStream()
}

// Config configures the demux loop.
type Config struct {
	Reader PacketReader
	Sink   PacketSink
}

// Run starts the demux loop. It reads packets from the reader and pushes them
// to the sink. It stops when:
//   - ctx is cancelled (viewer disconnect, seek, etc.)
//   - reader returns io.EOF (VOD end)
//   - a non-recoverable error occurs
//
// On EOF, it calls sink.EndOfStream() before returning.
// Returns nil on clean EOF or context cancellation, error otherwise.
func Run(ctx context.Context, cfg Config) error {
	reader := cfg.Reader
	sink := cfg.Sink

	for {
		// Check context before blocking on read.
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		pkt, err := reader.ReadPacket()
		if err != nil {
			if err == io.EOF {
				sink.EndOfStream()
				return nil
			}
			return fmt.Errorf("demuxloop: %w", err)
		}

		// Check context again after read (read may have blocked).
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		switch pkt.Type {
		case demux.Video:
			if err := sink.PushVideo(pkt.Data, pkt.PTS, pkt.DTS, pkt.Keyframe); err != nil {
				return fmt.Errorf("demuxloop: push video: %w", err)
			}
		case demux.Audio:
			if err := sink.PushAudio(pkt.Data, pkt.PTS, pkt.DTS); err != nil {
				return fmt.Errorf("demuxloop: push audio: %w", err)
			}
		case demux.Subtitle:
			if err := sink.PushSubtitle(pkt.Data, pkt.PTS, pkt.Duration); err != nil {
				return fmt.Errorf("demuxloop: push subtitle: %w", err)
			}
		}
	}
}
