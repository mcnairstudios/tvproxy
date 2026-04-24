package session

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demux"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demuxloop"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/probe"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/selector"
	"github.com/gavinmcnair/tvproxy/pkg/proto"
)

type DemuxSession struct {
	info     *probe.StreamInfo
	demuxer  *demux.Demuxer
	audioIdx int
	log      zerolog.Logger
}

type DemuxOpts struct {
	URL              string
	OutputDir        string
	AudioLanguage    string
	IsFileSource     bool
	Follow           bool
	FormatHint       string
	TimeoutSec       int
	UserAgent        string
	RTSPLatency      int
	CachedStreamInfo *probe.StreamInfo
	Log              zerolog.Logger
}

func NewDemuxSession(opts DemuxOpts) (*DemuxSession, error) {
	demuxOpts := demux.DemuxOpts{
		TimeoutSec:       max(opts.TimeoutSec, 5),
		AudioTrack:       -1,
		AudioLanguage:    opts.AudioLanguage,
		Follow:           opts.Follow,
		FormatHint:       opts.FormatHint,
		UserAgent:        opts.UserAgent,
		RTSPLatency:      opts.RTSPLatency,
		CachedStreamInfo: opts.CachedStreamInfo,
	}

	demuxer, err := demux.NewDemuxer(opts.URL, demuxOpts)
	if err != nil {
		return nil, fmt.Errorf("avdemux: %w", err)
	}

	info := demuxer.StreamInfo()
	audioIdx := selectAudioTrack(info, opts.AudioLanguage)
	if audioIdx >= 0 {
		demuxer.SetAudioTrack(audioIdx)
	}

	probePath := filepath.Join(opts.OutputDir, "probe.pb")
	if err := proto.WriteProbeFile(probePath, info, audioIdx); err != nil {
		opts.Log.Warn().Err(err).Msg("failed to write probe.pb")
	}

	return &DemuxSession{
		info:     info,
		demuxer:  demuxer,
		audioIdx: audioIdx,
		log:      opts.Log,
	}, nil
}

func (ds *DemuxSession) Info() *probe.StreamInfo {
	return ds.info
}

func (ds *DemuxSession) AudioIndex() int {
	return ds.audioIdx
}

func (ds *DemuxSession) RunWithSink(ctx context.Context, sink demuxloop.PacketSink) error {
	return demuxloop.Run(ctx, demuxloop.Config{
		Reader: ds.demuxer,
		Sink:   sink,
	})
}

func (ds *DemuxSession) Demuxer() *demux.Demuxer {
	return ds.demuxer
}

func (ds *DemuxSession) SetAudioTrack(idx int) error {
	return ds.demuxer.SetAudioTrack(idx)
}

func (ds *DemuxSession) SeekTo(posMs int64) error {
	return ds.demuxer.SeekTo(posMs)
}

func (ds *DemuxSession) Close() {
	if ds.demuxer != nil {
		ds.demuxer.Close()
	}
}

func selectAudioTrack(info *probe.StreamInfo, language string) int {
	if len(info.AudioTracks) == 0 {
		return -1
	}
	tracks := make([]selector.AudioTrack, len(info.AudioTracks))
	for i, t := range info.AudioTracks {
		tracks[i] = selector.AudioTrack{
			Index:       t.Index,
			Codec:       t.Codec,
			Channels:    t.Channels,
			SampleRate:  t.SampleRate,
			Language:    t.Language,
			IsAD:        t.IsAD,
			BitrateKbps: t.BitrateKbps,
		}
	}
	return selector.SelectAudio(tracks, selector.AudioPrefs{Language: language})
}
