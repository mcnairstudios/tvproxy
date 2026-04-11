package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-gst/go-gst/gst"
)

func main() {
	gst.Init(nil)

	url := "http://192.168.1.186:5004/auto/v101"
	out := "/tmp/hdhr_fast.mp4"
	os.Remove(out)

	pipeline, _ := gst.NewPipeline("hdhr-fast")

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

	mux, _ := gst.NewElement("isofmp4mux")
	mux.SetProperty("fragment-duration", uint(1000))
	sink, _ := gst.NewElement("filesink")
	sink.SetProperty("location", out)

	pipeline.AddMany(
		src, tsparse, demux,
		vQueue, vParse,
		aQueue, aInParse, aDec, aConv, aResample, aCaps, aEnc, aOutParse,
		mux, sink,
	)

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
		fmt.Printf("pad-added: %s\n", name)

		if strings.HasPrefix(name, "video") && !videoLinked {
			ret := pad.Link(vQueue.GetStaticPad("sink"))
			fmt.Printf("Video: %v\n", ret)
			videoLinked = true
		} else if strings.Contains(name, "audio") && !audioLinked {
			ret := pad.Link(aQueue.GetStaticPad("sink"))
			fmt.Printf("Audio: %v\n", ret)
			audioLinked = true
		}
	})

	start := time.Now()
	pipeline.SetState(gst.StatePlaying)
	fmt.Println("tsdemux + aacparse + isofmp4mux (30s)...")

	go func() {
		for {
			info, err := os.Stat(out)
			if err == nil && info.Size() > 0 {
				fmt.Printf("FIRST BYTE: %v\n", time.Since(start))
				return
			}
			if time.Since(start) > 30*time.Second {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
	case <-time.After(30 * time.Second):
	}

	pipeline.SetState(gst.StateNull)

	info, _ := os.Stat(out)
	if info != nil && info.Size() > 0 {
		fmt.Printf("Total: %d bytes in %v\n", info.Size(), time.Since(start))
		probe := exec.Command("ffprobe", "-v", "quiet", "-show_entries", "stream=codec_name,codec_type,channels", "-of", "compact", out)
		probeOut, _ := probe.Output()
		fmt.Println(string(probeOut))
	} else {
		fmt.Println("NO OUTPUT")
	}
	fmt.Println("Play: vlc", out)
}
