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

	var isKeyframe func([]byte) bool
	switch store.videoCodec {
	case "h265":
		isKeyframe = IsHEVCKeyframe
	case "av1":
		isKeyframe = IsAV1Keyframe
	default:
		isKeyframe = IsH264Keyframe
	}

	sink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(s *app.Sink) gst.FlowReturn {
			sample := s.PullSample()
			if sample == nil {
				return gst.FlowEOS
			}
			buf := sample.GetBuffer()
			data := buf.Bytes()

			if store.videoCodec == "av1" && len(data) <= 10 {
				return gst.FlowOK
			}

			isDelta := buf.HasFlags(gst.BufferFlagDeltaUnit)
			bufKeyframe := !isDelta
			if !bufKeyframe {
				bufKeyframe = isKeyframe(data)
			}

			ptsNs := int64(-1)
			if rawPTS := buf.PresentationTimestamp(); rawPTS != gst.ClockTimeNone {
				ptsNs = int64(rawPTS)
			}
			bufDurNs := int64(-1)
			if d := buf.Duration(); d != gst.ClockTimeNone {
				bufDurNs = int64(d)
			}

			store.PushVideoFrame(data, ptsNs, bufDurNs, bufKeyframe)
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

			ptsNs := int64(-1)
			if rawPTS := buf.PresentationTimestamp(); rawPTS != gst.ClockTimeNone {
				ptsNs = int64(rawPTS)
			}
			bufDurNs := int64(-1)
			if d := buf.Duration(); d != gst.ClockTimeNone {
				bufDurNs = int64(d)
			}

			store.PushAudioFrame(data, ptsNs, bufDurNs)
			return gst.FlowOK
		},
	})
	return nil
}
