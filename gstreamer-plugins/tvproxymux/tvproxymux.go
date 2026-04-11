//lint:file-ignore U1000 Ignore all unused code

// +plugin:Name=tvproxymux
// +plugin:Description=TVProxy muxer bin with auto codec detection and proven settings
// +plugin:Version=v0.0.1
// +plugin:License=gst.LicenseLGPL
// +plugin:Source=tvproxymux
// +plugin:Package=tvproxy
// +plugin:Origin=https://github.com/gavinmcnair/tvproxymux
// +plugin:ReleaseDate=2026-04-11
//
// +element:Name=tvproxymux
// +element:Rank=gst.RankNone
// +element:Impl=tvProxyMux
// +element:Subclass=gst.ExtendsBin
// +element:Interfaces=gst.InterfaceChildProxy
//
//go:generate gst-plugin-gen
package main

import "C"

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-gst/go-glib/glib"
	"github.com/go-gst/go-gst/gst"
)

func main() {}

var CAT = gst.NewDebugCategory(
	"tvproxymux",
	gst.DebugColorNone,
	"TVProxy Muxer Bin",
)

const (
	propOutputFormat uint = iota
	propFragmentDuration
)

var properties = []*glib.ParamSpec{
	glib.NewStringParam(
		"output-format",
		"Output Format",
		"Output container format: mp4 or mpegts",
		nil,
		glib.ParameterReadWrite,
	),
	glib.NewUintParam(
		"fragment-duration",
		"Fragment Duration",
		"Fragment duration in ms for MP4 output (ignored for mpegts)",
		uint(100),
		uint(1000),
		uint(500),
		glib.ParameterReadWrite,
	),
}

type tvProxyMux struct {
	mu sync.Mutex

	outputFormat     string
	fragmentDuration uint

	// Internal elements
	mux       *gst.Element
	videoParser *gst.Element
	audioParser *gst.Element

	// Ghost pads
	srcGhostPad   *gst.GhostPad
	videoGhostPad *gst.GhostPad
	audioGhostPad *gst.GhostPad

	// Track whether internal pipeline is built
	videoLinked bool
	audioLinked bool
}

func (t *tvProxyMux) New() glib.GoObjectSubclass {
	return &tvProxyMux{
		outputFormat:     "mp4",
		fragmentDuration: 500,
	}
}

func (t *tvProxyMux) ClassInit(klass *glib.ObjectClass) {
	class := gst.ToElementClass(klass)
	class.SetMetadata(
		"TVProxy Muxer",
		"Codec/Muxer/Bin",
		"Auto-detecting muxer bin with proven settings for live video",
		"Gavin McNair",
	)

	// Video sink request pad: accepts h264, h265, mpeg2
	class.AddPadTemplate(gst.NewPadTemplate(
		"video",
		gst.PadDirectionSink,
		gst.PadPresenceRequest,
		gst.NewCapsFromString(
			"video/x-h264; video/x-h265; video/mpeg, mpegversion=(int)2",
		),
	))

	// Audio sink request pad: accepts AAC
	class.AddPadTemplate(gst.NewPadTemplate(
		"audio",
		gst.PadDirectionSink,
		gst.PadPresenceRequest,
		gst.NewCapsFromString(
			"audio/mpeg, mpegversion=(int){2,4}; audio/x-ac3; audio/x-eac3",
		),
	))

	// Source pad
	class.AddPadTemplate(gst.NewPadTemplate(
		"src",
		gst.PadDirectionSource,
		gst.PadPresenceAlways,
		gst.NewCapsFromString("video/quicktime; video/mpegts"),
	))

	class.InstallProperties(properties)
}

func (t *tvProxyMux) SetProperty(self *glib.Object, id uint, value *glib.Value) {
	switch id {
	case propOutputFormat:
		if value != nil {
			if val, err := value.GetString(); err == nil {
				if val == "mp4" || val == "mpegts" {
					t.outputFormat = val
				}
			}
		}
	case propFragmentDuration:
		if value != nil {
			if gv, err := value.GoValue(); err == nil {
				if v, ok := gv.(uint); ok && v >= 100 && v <= 1000 {
					t.fragmentDuration = v
				}
			}
		}
	}
}

func (t *tvProxyMux) GetProperty(self *glib.Object, id uint) *glib.Value {
	switch id {
	case propOutputFormat:
		v, err := glib.GValue(t.outputFormat)
		if err == nil {
			return v
		}
	case propFragmentDuration:
		v, err := glib.GValue(t.fragmentDuration)
		if err == nil {
			return v
		}
	}
	return nil
}

func (t *tvProxyMux) InstanceInit(instance *glib.Object) {
	// We create the src ghost pad with no target here.
	// The target gets set when the mux element is created in buildPipeline.
	self := gst.ToGstBin(instance)
	klass := instance.Class()
	class := gst.ToElementClass(klass)

	t.srcGhostPad = gst.NewGhostPadNoTargetFromTemplate("src", class.GetPadTemplate("src"))
	self.AddPad(t.srcGhostPad.Pad)
}

func (t *tvProxyMux) Constructed(o *glib.Object) {}

// RequestNewPad handles creation of video and audio sink pads
func (t *tvProxyMux) RequestNewPad(self *gst.Element, templ *gst.PadTemplate, name string, caps *gst.Caps) *gst.Pad {
	t.mu.Lock()
	defer t.mu.Unlock()

	bin := gst.ToGstBin(self)
	templName := templ.Name()

	switch {
	case strings.HasPrefix(templName, "video"):
		if t.videoLinked {
			CAT.Log(gst.LevelWarning, "Video pad already requested")
			return nil
		}
		return t.setupVideoPad(bin, self, templ, caps)

	case strings.HasPrefix(templName, "audio"):
		if t.audioLinked {
			CAT.Log(gst.LevelWarning, "Audio pad already requested")
			return nil
		}
		return t.setupAudioPad(bin, self, templ, caps)
	}

	return nil
}

// ReleasePad cleans up when a pad is released
func (t *tvProxyMux) ReleasePad(self *gst.Element, pad *gst.Pad) {
	t.mu.Lock()
	defer t.mu.Unlock()

	padName := pad.GetName()
	if strings.HasPrefix(padName, "video") {
		t.videoLinked = false
	} else if strings.HasPrefix(padName, "audio") {
		t.audioLinked = false
	}
	self.RemovePad(pad)
}

func (t *tvProxyMux) ensureMux(bin *gst.Bin) error {
	if t.mux != nil {
		return nil
	}

	var err error
	if t.outputFormat == "mpegts" {
		t.mux, err = gst.NewElement("mpegtsmux")
		if err != nil {
			return fmt.Errorf("failed to create mpegtsmux: %w", err)
		}
		// DO NOT set latency=0 or alignment=-1 — breaks video
	} else {
		t.mux, err = gst.NewElement("mp4mux")
		if err != nil {
			return fmt.Errorf("failed to create mp4mux: %w", err)
		}
		// Proven settings — DO NOT CHANGE
		t.mux.SetProperty("fragment-duration", t.fragmentDuration)
		t.mux.SetProperty("streamable", true)
	}

	bin.Add(t.mux)

	// Set the ghost pad target to mux src
	muxSrc := t.mux.GetStaticPad("src")
	t.srcGhostPad.SetTarget(muxSrc)

	t.mux.SyncStateWithParent()

	return nil
}

func (t *tvProxyMux) detectVideoCodec(caps *gst.Caps) string {
	if caps == nil {
		return ""
	}
	capsStr := caps.String()
	if strings.Contains(capsStr, "video/x-h264") {
		return "h264"
	}
	if strings.Contains(capsStr, "video/x-h265") {
		return "h265"
	}
	if strings.Contains(capsStr, "video/mpeg") {
		return "mpeg2"
	}
	return ""
}

func (t *tvProxyMux) createParser(codec string) (*gst.Element, error) {
	switch codec {
	case "h264":
		parser, err := gst.NewElement("h264parse")
		if err != nil {
			return nil, err
		}
		// CRITICAL: config-interval=-1 forces VPS/SPS/PPS on every keyframe
		parser.SetProperty("config-interval", -1)
		return parser, nil
	case "h265":
		parser, err := gst.NewElement("h265parse")
		if err != nil {
			return nil, err
		}
		// CRITICAL: config-interval=-1 forces VPS/SPS/PPS on every keyframe
		parser.SetProperty("config-interval", -1)
		return parser, nil
	case "mpeg2":
		parser, err := gst.NewElement("mpegvideoparse")
		if err != nil {
			return nil, err
		}
		// No config-interval needed for mpeg2
		return parser, nil
	default:
		return nil, fmt.Errorf("unsupported video codec: %s", codec)
	}
}

func (t *tvProxyMux) setupVideoPad(bin *gst.Bin, self *gst.Element, templ *gst.PadTemplate, caps *gst.Caps) *gst.Pad {
	if err := t.ensureMux(bin); err != nil {
		CAT.Log(gst.LevelError, fmt.Sprintf("Failed to create mux: %s", err))
		return nil
	}

	// Get a request pad on the mux for video
	muxSinkPad := t.mux.GetRequestPad("video_%u")
	if muxSinkPad == nil {
		muxSinkPad = t.mux.GetRequestPad("sink_%d")
	}
	if muxSinkPad == nil {
		CAT.Log(gst.LevelError, "Failed to get video sink pad from mux")
		return nil
	}

	// Try to detect codec from caps provided at request time
	codec := t.detectVideoCodec(caps)

	if codec != "" {
		// Caps available — build the chain immediately
		return t.buildVideoChain(bin, templ, codec, muxSinkPad)
	}

	// Caps not available yet — create ghost pad with no target,
	// use a probe to detect codec and build chain on first caps event
	return t.buildVideoChainDeferred(bin, templ, muxSinkPad)
}

func (t *tvProxyMux) buildVideoChain(bin *gst.Bin, templ *gst.PadTemplate, codec string, muxSinkPad *gst.Pad) *gst.Pad {
	parser, err := t.createParser(codec)
	if err != nil {
		CAT.Log(gst.LevelError, fmt.Sprintf("Failed to create parser for %s: %s", codec, err))
		return nil
	}
	t.videoParser = parser

	bin.Add(parser)

	// Link parser src to mux sink
	parserSrc := parser.GetStaticPad("src")
	parserSrc.Link(muxSinkPad)

	parser.SyncStateWithParent()

	// Create ghost pad targeting parser sink
	parserSink := parser.GetStaticPad("sink")
	ghostPad := gst.NewGhostPadFromTemplate("video", parserSink, templ)
	t.videoGhostPad = ghostPad

	bin.AddPad(ghostPad.Pad)
	t.videoLinked = true

	CAT.Log(gst.LevelInfo, fmt.Sprintf("Video chain built with %s parser", codec))
	return ghostPad.Pad
}

func (t *tvProxyMux) buildVideoChainDeferred(bin *gst.Bin, templ *gst.PadTemplate, muxSinkPad *gst.Pad) *gst.Pad {
	// Create a ghost pad with no target — we'll set the target once we know the codec
	ghostPad := gst.NewGhostPadNoTargetFromTemplate("video", templ)
	t.videoGhostPad = ghostPad

	// Add an event probe on the ghost pad's internal proxy pad to detect caps
	ghostPad.Pad.AddProbe(gst.PadProbeTypeEventDownstream, func(pad *gst.Pad, info *gst.PadProbeInfo) gst.PadProbeReturn {
		event := info.GetEvent()
		if event == nil {
			return gst.PadProbeOK
		}
		if event.Type() != gst.EventTypeCaps {
			return gst.PadProbeOK
		}

		caps := event.ParseCaps()
		codec := t.detectVideoCodec(caps)
		if codec == "" {
			return gst.PadProbeOK
		}

		CAT.Log(gst.LevelInfo, fmt.Sprintf("Deferred video codec detection: %s", codec))

		parser, err := t.createParser(codec)
		if err != nil {
			CAT.Log(gst.LevelError, fmt.Sprintf("Failed to create parser: %s", err))
			return gst.PadProbeOK
		}
		t.videoParser = parser

		bin.Add(parser)

		// Link parser → mux
		parserSrcPad := parser.GetStaticPad("src")
		parserSrcPad.Link(muxSinkPad)

		parser.SyncStateWithParent()

		// Set the ghost pad target to parser sink
		parserSink := parser.GetStaticPad("sink")
		ghostPad.SetTarget(parserSink)

		return gst.PadProbeRemove
	})

	bin.AddPad(ghostPad.Pad)
	t.videoLinked = true

	CAT.Log(gst.LevelInfo, "Video chain built with deferred codec detection")
	return ghostPad.Pad
}

func (t *tvProxyMux) setupAudioPad(bin *gst.Bin, self *gst.Element, templ *gst.PadTemplate, caps *gst.Caps) *gst.Pad {
	if err := t.ensureMux(bin); err != nil {
		CAT.Log(gst.LevelError, fmt.Sprintf("Failed to create mux: %s", err))
		return nil
	}

	// Audio always goes through aacparse
	aacparse, err := gst.NewElement("aacparse")
	if err != nil {
		CAT.Log(gst.LevelError, fmt.Sprintf("Failed to create aacparse: %s", err))
		return nil
	}
	t.audioParser = aacparse

	bin.Add(aacparse)

	// Get audio request pad on mux
	muxSinkPad := t.mux.GetRequestPad("audio_%u")
	if muxSinkPad == nil {
		muxSinkPad = t.mux.GetRequestPad("sink_%d")
	}
	if muxSinkPad == nil {
		CAT.Log(gst.LevelError, "Failed to get audio sink pad from mux")
		return nil
	}

	// Link aacparse → mux
	aacparseSrc := aacparse.GetStaticPad("src")
	aacparseSrc.Link(muxSinkPad)

	aacparse.SyncStateWithParent()

	// Create ghost pad targeting aacparse sink
	aacparseSink := aacparse.GetStaticPad("sink")
	ghostPad := gst.NewGhostPadFromTemplate("audio", aacparseSink, templ)
	t.audioGhostPad = ghostPad

	bin.AddPad(ghostPad.Pad)
	t.audioLinked = true

	CAT.Log(gst.LevelInfo, "Audio chain built with aacparse")
	return ghostPad.Pad
}
