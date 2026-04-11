package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-gst/go-gst/gst"
)

func test(name string) {
	url := "rtsp://192.168.1.149/?freq=545.833&msys=dvbt2&mtype=256qam&pids=0,6650,6601,6602,6606,6605,7302,7319,7108,7110&bw=8&plp=0"
	out := "/tmp/satip_" + name + ".ts"
	os.Remove(out)

	pipeline, _ := gst.NewPipeline(name)
	src, _ := gst.NewElement("rtspsrc")
	src.SetProperty("location", url)
	src.SetProperty("latency", uint(0))
	src.SetProperty("protocols", uint(4))
	src.SetProperty("buffer-mode", uint(0))

	depay, _ := gst.NewElement("rtpmp2tdepay")
	tsparse, _ := gst.NewElement("tsparse")
	tsparse.SetProperty("set-timestamps", true)
	demux, _ := gst.NewElement("tsdemux")
	vQueue, _ := gst.NewElement("queue")
	vParse, _ := gst.NewElement("h264parse")
	vParse.SetProperty("config-interval", -1)
	aQueue, _ := gst.NewElement("queue")
	aInParse, _ := gst.NewElement("aacparse")
	aDec, _ := gst.NewElement("avdec_aac_latm")
	aConv, _ := gst.NewElement("audioconvert")
	aResample, _ := gst.NewElement("audioresample")
	aCaps, _ := gst.NewElement("capsfilter")
	aCaps.SetProperty("caps", gst.NewCapsFromString("audio/x-raw,channels=2"))
	aEnc, _ := gst.NewElement("faac")
	aOutParse, _ := gst.NewElement("aacparse")
	mux, _ := gst.NewElement("mpegtsmux")
	sink, _ := gst.NewElement("filesink")
	sink.SetProperty("location", out)

	pipeline.AddMany(src, depay, tsparse, demux, vQueue, vParse, aQueue, aInParse, aDec, aConv, aResample, aCaps, aEnc, aOutParse, mux, sink)
	src.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
		sinkPad := depay.GetStaticPad("sink")
		if sinkPad != nil && !sinkPad.IsLinked() { pad.Link(sinkPad) }
	})
	gst.ElementLinkMany(depay, tsparse, demux)
	gst.ElementLinkMany(vQueue, vParse, mux)
	gst.ElementLinkMany(aQueue, aInParse, aDec, aConv, aResample, aCaps, aEnc, aOutParse, mux)
	gst.ElementLinkMany(mux, sink)

	videoLinked := false
	audioLinked := false
	demux.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
		caps := pad.GetCurrentCaps()
		if caps == nil { return }
		n := caps.GetStructureAt(0).Name()
		if strings.HasPrefix(n, "video") && !videoLinked { pad.Link(vQueue.GetStaticPad("sink")); videoLinked = true }
		if strings.Contains(n, "audio") && !audioLinked { pad.Link(aQueue.GetStaticPad("sink")); audioLinked = true }
	})

	start := time.Now()
	pipeline.SetState(gst.StatePlaying)

	for {
		info, _ := os.Stat(out)
		if info != nil && info.Size() > 0 {
			fmt.Printf("  %s: %v", name, time.Since(start))
			break
		}
		if time.Since(start) > 20*time.Second { fmt.Printf("  %s: TIMEOUT", name); break }
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(3 * time.Second)
	pipeline.SetState(gst.StateNull)

	info, _ := os.Stat(out)
	if info != nil {
		probe := exec.Command("ffprobe", "-v", "quiet", "-show_entries", "stream=codec_name,codec_type", "-of", "compact", out)
		o, _ := probe.Output()
		fmt.Printf(" | %d bytes | %s\n", info.Size(), strings.TrimSpace(strings.Split(string(o), "\n")[0]))
	}
	time.Sleep(1 * time.Second)
}

func main() {
	gst.Init(nil)
	fmt.Println("Cold vs warm start:")
	test("cold")
	test("warm")
	// Different frequency to force tuner retune
	fmt.Println("Different freq then back:")
	test("retune")
}
