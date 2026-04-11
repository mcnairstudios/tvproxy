package gstreamer

import (
	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type TranscoderChoice struct {
	UseGStreamer bool
	Pipeline     *Pipeline
	Reason       string
}

func ShouldUseGStreamer(
	probeCache store.ProbeCache,
	streamURL string,
	hwAccel HWAccel,
) TranscoderChoice {
	if !Available() {
		return TranscoderChoice{UseGStreamer: false, Reason: "gst-launch-1.0 not found"}
	}

	if probeCache == nil {
		return TranscoderChoice{UseGStreamer: false, Reason: "no probe cache"}
	}

	hash := ffmpeg.StreamHash(streamURL)
	probe, err := probeCache.GetProbe(hash)
	if err != nil || probe == nil {
		probe, err = probeCache.GetProbeByStreamID(hash)
		if err != nil || probe == nil {
			return TranscoderChoice{UseGStreamer: false, Reason: "no cached probe data"}
		}
	}

	if probe.Video == nil {
		return TranscoderChoice{UseGStreamer: false, Reason: "no video in probe"}
	}

	vcodec := normalizeCodec(probe.Video.Codec)
	if vcodec != "h264" && vcodec != "h265" && vcodec != "mpeg2video" {
		return TranscoderChoice{UseGStreamer: false, Reason: "unsupported video codec: " + vcodec}
	}

	acodec := ""
	if len(probe.AudioTracks) > 0 {
		acodec = normalizeCodec(probe.AudioTracks[0].Codec)
	}

	pipeline := BuildFromProbe(probe, streamURL, PipelineOpts{
		InputType:        "http",
		IsLive:           true,
		OutputVideoCodec: "copy",
		OutputAudioCodec: "aac",
		OutputFormat:     OutputMPEGTS,
		HWAccel:          hwAccel,
	})

	return TranscoderChoice{
		UseGStreamer: true,
		Pipeline:    pipeline,
		Reason:      "probe data available: " + vcodec + "/" + acodec,
	}
}
