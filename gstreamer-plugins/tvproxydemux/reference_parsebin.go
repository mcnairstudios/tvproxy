package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-gst/go-gst/gst"
)

func main() {
	gst.Init(nil)

	url := "http://192.168.1.186:5004/auto/v101"
	out := "/tmp/hdhr_out.ts"
	os.Remove(out)

	pipelineStr := fmt.Sprintf(
		"souphttpsrc location=%s do-timestamp=true is-live=true "+
			"! parsebin name=demux "+
			"demux. ! video/x-h264 ! queue ! vtdec "+
			"! vtenc_h265 bitrate=4000 realtime=true allow-frame-reordering=false "+
			"! h265parse config-interval=-1 ! mpegtsmux name=mux ! filesink location=%s "+
			"demux. ! audio/mpeg ! queue ! avdec_aac_latm ! audioconvert ! audioresample "+
			"! audio/x-raw,channels=2 ! faac ! aacparse ! mux.",
		url, out)

	fmt.Printf("Pipeline: %s\n\n", pipelineStr)

	pipeline, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		fmt.Printf("Failed to create pipeline: %v\n", err)
		return
	}

	start := time.Now()
	pipeline.SetState(gst.StatePlaying)
	fmt.Println("Streaming v101... 30 seconds.")

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
