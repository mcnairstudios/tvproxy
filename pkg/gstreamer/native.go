package gstreamer

import (
	"fmt"

	"github.com/go-gst/go-gst/gst"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

func init() {
	gst.Init(nil)
}

func BuildNativePipeline(name string, probe *media.ProbeResult, opts PipelineOpts) (*gst.Pipeline, error) {
	if probe != nil {
		if probe.Video != nil {
			opts.VideoCodec = probe.Video.Codec
		}
		if len(probe.AudioTracks) > 0 {
			opts.AudioCodec = probe.AudioTracks[0].Codec
		}
		opts.Container = probe.FormatName
	}
	opts.InputURL = opts.InputURL
	pstr := buildPipelineStr(opts)
	pipeline, err := gst.NewPipelineFromString(pstr)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline from: %s: %w", pstr, err)
	}
	return pipeline, nil
}
