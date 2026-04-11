// tvproxysrc is a GStreamer source bin that wraps HTTP, RTSP, and file inputs
// behind a single element with a static source pad outputting video/mpegts.
//
// +plugin:Name=tvproxysrc
// +plugin:Description=TVProxy source bin for HTTP/RTSP/file MPEG-TS inputs
// +plugin:Version=v0.1.0
// +plugin:License=gst.LicenseLGPL
// +plugin:Source=tvproxysrc
// +plugin:Package=tvproxysrc
// +plugin:Origin=https://github.com/gavinmcnair/tvproxysrc
// +plugin:ReleaseDate=2026-04-11
//
// +element:Name=tvproxysrc
// +element:Rank=gst.RankNone
// +element:Impl=tvProxySrc
// +element:Subclass=gst.ExtendsBin
//
//go:generate gst-plugin-gen
package main

import (
	"fmt"
	"strings"

	"github.com/go-gst/go-glib/glib"
	"github.com/go-gst/go-gst/gst"
)

func main() {}

var CAT = gst.NewDebugCategory(
	"tvproxysrc",
	gst.DebugColorNone,
	"TVProxy Source Element",
)

var properties = []*glib.ParamSpec{
	glib.NewStringParam(
		"location",
		"Location",
		"URI or file path to read from",
		nil,
		glib.ParameterReadWrite,
	),
	glib.NewBoolParam(
		"is-live",
		"Is Live",
		"Whether this is a live source",
		true,
		glib.ParameterReadWrite,
	),
	glib.NewStringParam(
		"rtsp-transport",
		"RTSP Transport",
		"RTSP transport protocol: tcp or udp",
		strPtr("tcp"),
		glib.ParameterReadWrite,
	),
}

func strPtr(s string) *string { return &s }

type settings struct {
	location      string
	isLive        bool
	rtspTransport string
}

type tvProxySrc struct {
	settings *settings
	ghostPad *gst.GhostPad
	built    bool
}

func (t *tvProxySrc) New() glib.GoObjectSubclass {
	return &tvProxySrc{
		settings: &settings{
			location:      "",
			isLive:        true,
			rtspTransport: "tcp",
		},
	}
}

func (t *tvProxySrc) ClassInit(klass *glib.ObjectClass) {
	class := gst.ToElementClass(klass)
	class.SetMetadata(
		"TVProxy Source",
		"Source/Bin",
		"Source bin for HTTP/RTSP/file MPEG-TS inputs",
		"Gavin McNair",
	)
	class.AddPadTemplate(gst.NewPadTemplate(
		"src",
		gst.PadDirectionSource,
		gst.PadPresenceAlways,
		gst.NewCapsFromString("video/mpegts"),
	))
	class.InstallProperties(properties)
}

func (t *tvProxySrc) InstanceInit(instance *glib.Object) {
	klass := instance.Class()
	class := gst.ToElementClass(klass)
	t.ghostPad = gst.NewGhostPadNoTargetFromTemplate("src", class.GetPadTemplate("src"))
	bin := gst.ToGstBin(instance)
	bin.AddPad(t.ghostPad.Pad)
}

func (t *tvProxySrc) Constructed(self *glib.Object) {}

func (t *tvProxySrc) SetProperty(self *glib.Object, id uint, value *glib.Value) {
	param := properties[id]
	switch param.Name() {
	case "location":
		var val string
		if value != nil {
			val, _ = value.GetString()
		}
		t.settings.location = val
		elem := gst.ToElement(self)
		elem.Log(CAT, gst.LevelInfo, fmt.Sprintf("Set location to %s", val))

		if val != "" && !t.built {
			t.buildPipeline(self)
		}

	case "is-live":
		if value != nil {
			val, _ := value.GoValue()
			if b, ok := val.(bool); ok {
				t.settings.isLive = b
			}
		}

	case "rtsp-transport":
		if value != nil {
			val, _ := value.GetString()
			t.settings.rtspTransport = val
		}
	}
}

func (t *tvProxySrc) GetProperty(self *glib.Object, id uint) *glib.Value {
	param := properties[id]
	switch param.Name() {
	case "location":
		if t.settings.location == "" {
			return nil
		}
		val, err := glib.GValue(t.settings.location)
		if err == nil {
			return val
		}
	case "is-live":
		val, err := glib.GValue(t.settings.isLive)
		if err == nil {
			return val
		}
	case "rtsp-transport":
		val, err := glib.GValue(t.settings.rtspTransport)
		if err == nil {
			return val
		}
	}
	return nil
}

func (t *tvProxySrc) buildPipeline(self *glib.Object) {
	bin := gst.ToGstBin(self)
	elem := gst.ToElement(self)
	loc := t.settings.location

	switch {
	case strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://"):
		t.buildHTTP(bin, elem)
	case strings.HasPrefix(loc, "rtsp://") || strings.HasPrefix(loc, "rtsps://"):
		t.buildRTSP(bin, elem)
	default:
		t.buildFile(bin, elem)
	}
	t.built = true
}

func (t *tvProxySrc) buildHTTP(bin *gst.Bin, elem *gst.Element) {
	src, err := gst.NewElement("souphttpsrc")
	if err != nil {
		elem.ErrorMessage(gst.DomainResource, gst.ResourceErrorSettings,
			"Failed to create souphttpsrc", err.Error())
		return
	}
	src.SetProperty("location", t.settings.location)
	src.SetProperty("do-timestamp", t.settings.isLive)
	src.SetProperty("is-live", t.settings.isLive)

	bin.Add(src)
	srcPad := src.GetStaticPad("src")
	t.ghostPad.SetTarget(srcPad)
	elem.Log(CAT, gst.LevelInfo, "Built HTTP source pipeline")
}

func (t *tvProxySrc) buildRTSP(bin *gst.Bin, elem *gst.Element) {
	src, err := gst.NewElement("rtspsrc")
	if err != nil {
		elem.ErrorMessage(gst.DomainResource, gst.ResourceErrorSettings,
			"Failed to create rtspsrc", err.Error())
		return
	}
	src.SetProperty("location", t.settings.location)
	src.SetProperty("latency", uint(0))

	// protocols: 4 = TCP, 1 = UDP
	protocols := uint(4)
	if t.settings.rtspTransport == "udp" {
		protocols = 1
	}
	src.SetProperty("protocols", protocols)

	depay, err := gst.NewElement("rtpmp2tdepay")
	if err != nil {
		elem.ErrorMessage(gst.DomainResource, gst.ResourceErrorSettings,
			"Failed to create rtpmp2tdepay", err.Error())
		return
	}

	bin.AddMany(src, depay)

	// Ghost the depayloader's src pad - it's static and always available
	depaySrcPad := depay.GetStaticPad("src")
	t.ghostPad.SetTarget(depaySrcPad)

	// Handle rtspsrc's dynamic pads
	src.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
		sinkPad := depay.GetStaticPad("sink")
		if sinkPad != nil && !sinkPad.IsLinked() {
			pad.Link(sinkPad)
			elem.Log(CAT, gst.LevelInfo, "Linked rtspsrc pad to rtpmp2tdepay")
		}
	})

	elem.Log(CAT, gst.LevelInfo, "Built RTSP source pipeline")
}

func (t *tvProxySrc) buildFile(bin *gst.Bin, elem *gst.Element) {
	src, err := gst.NewElement("filesrc")
	if err != nil {
		elem.ErrorMessage(gst.DomainResource, gst.ResourceErrorSettings,
			"Failed to create filesrc", err.Error())
		return
	}
	src.SetProperty("location", t.settings.location)

	bin.Add(src)
	srcPad := src.GetStaticPad("src")
	t.ghostPad.SetTarget(srcPad)
	elem.Log(CAT, gst.LevelInfo, "Built file source pipeline")
}
