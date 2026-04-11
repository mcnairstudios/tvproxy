package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-gst/go-gst/gst"
)

func main() {
	gst.Init(nil)
	url := "http://192.168.1.186:5004/auto/v101"
	out := "/tmp/hdhr_fast.mp4"
	os.Remove(out)

	pipeline, _ := gst.NewPipeline("fast")
	src, _ := gst.NewElement("souphttpsrc")
	src.SetProperty("location", url)
	src.SetProperty("do-timestamp", true)
	src.SetProperty("is-live", true)
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
	mux, _ := gst.NewElement("mp4mux")
	mux.SetProperty("fragment-duration", uint(500))
	mux.SetProperty("streamable", true)
	sink, _ := gst.NewElement("filesink")
	sink.SetProperty("location", out)

	pipeline.AddMany(src, tsparse, demux, vQueue, vParse, aQueue, aInParse, aDec, aConv, aResample, aCaps, aEnc, aOutParse, mux, sink)
	gst.ElementLinkMany(src, tsparse, demux)
	gst.ElementLinkMany(vQueue, vParse, mux)
	gst.ElementLinkMany(aQueue, aInParse, aDec, aConv, aResample, aCaps, aEnc, aOutParse, mux)
	gst.ElementLinkMany(mux, sink)

	videoLinked := false
	audioLinked := false
	demux.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
		caps := pad.GetCurrentCaps()
		if caps == nil { return }
		name := caps.GetStructureAt(0).Name()
		if strings.HasPrefix(name, "video") && !videoLinked {
			pad.Link(vQueue.GetStaticPad("sink"))
			videoLinked = true
			fmt.Println("Video linked")
		} else if strings.Contains(name, "audio") && !audioLinked {
			pad.Link(aQueue.GetStaticPad("sink"))
			audioLinked = true
			fmt.Println("Audio linked")
		}
	})

	start := time.Now()
	pipeline.SetState(gst.StatePlaying)

	for {
		info, _ := os.Stat(out)
		if info != nil && info.Size() > 0 {
			fmt.Printf("FIRST BYTE: %v\n", time.Since(start))
			break
		}
		if time.Since(start) > 20*time.Second { fmt.Println("TIMEOUT"); break }
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(10 * time.Second)
	pipeline.SetState(gst.StateNull)

	info, _ := os.Stat(out)
	if info != nil {
		fmt.Printf("Total: %d bytes\n", info.Size())
		p := exec.Command("ffprobe", "-v", "quiet", "-show_entries", "stream=codec_name,codec_type,channels", "-of", "compact", out)
		o, _ := p.Output()
		fmt.Println(string(o))
	}
}
