package fmp4

import (
	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
)

func SetupVideoSink(pipeline *gst.Pipeline, store *TrackStore) error {
	elem, err := pipeline.GetElementByName("videosink")
	if err != nil {
		return err
	}
	sink := app.SinkFromElement(elem)
	if sink == nil {
		return err
	}

	isKeyframe := IsH264Keyframe
	if store.videoCodec == "h265" {
		isKeyframe = IsHEVCKeyframe
	}

	sink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(s *app.Sink) gst.FlowReturn {
			sample := s.PullSample()
			if sample == nil {
				return gst.FlowEOS
			}
			buf := sample.GetBuffer()
			data := buf.Bytes()

			rawPTS := buf.PresentationTimestamp()
			pts := int64(rawPTS)
			var duration uint32
			if rawPTS != gst.ClockTimeNone && store.LastPTS >= 0 {
				diffNs := pts - store.LastPTS
				if diffNs > 0 && diffNs < 1e9 {
					duration = uint32(diffNs * int64(store.timescale) / 1e9)
				}
			}
			if rawPTS != gst.ClockTimeNone {
				store.LastPTS = pts
			}
			if duration == 0 {
				durNs := buf.Duration()
				duration = uint32(durNs * gst.ClockTime(store.timescale) / 1e9)
				if duration == 0 {
					duration = 3750 // ~24fps fallback
				}
			}

			store.PushVideoFrame(data, duration, isKeyframe(data))
			return gst.FlowOK
		},
	})
	return nil
}

func SetupAudioSink(pipeline *gst.Pipeline, store *TrackStore) error {
	elem, err := pipeline.GetElementByName("audiosink")
	if err != nil {
		return err
	}
	sink := app.SinkFromElement(elem)
	if sink == nil {
		return err
	}

	sink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(s *app.Sink) gst.FlowReturn {
			sample := s.PullSample()
			if sample == nil {
				return gst.FlowEOS
			}
			buf := sample.GetBuffer()
			data := buf.Bytes()

			rawPTS := buf.PresentationTimestamp()
			pts := int64(rawPTS)
			var duration uint32
			if rawPTS != gst.ClockTimeNone && store.LastPTS >= 0 {
				diffNs := pts - store.LastPTS
				if diffNs > 0 && diffNs < 1e9 {
					duration = uint32(diffNs * 48000 / 1e9)
				}
			}
			if rawPTS != gst.ClockTimeNone {
				store.LastPTS = pts
			}
			if duration == 0 {
				duration = 1024 // standard AAC frame size
			}

			store.PushAudioFrame(data, duration)
			return gst.FlowOK
		},
	})
	return nil
}
