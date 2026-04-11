package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/go-gst/go-gst/gst"
)

func main() {
	gst.Init(nil)

	url := "http://192.168.1.186:5004/auto/v101"
	out := "/tmp/hdhr_tvproxydemux.mp4"
	os.Remove(out)

	pipelineStr := fmt.Sprintf(
		"souphttpsrc location=%s do-timestamp=true is-live=true "+
			"! tvproxydemux name=d "+
			"d.video ! h264parse config-interval=-1 ! mp4mux name=mux fragment-duration=500 streamable=true ! filesink location=%s "+
			"d.audio ! mux.",
		url, out)

	fmt.Printf("Pipeline: %s\n\n", pipelineStr)

	pipeline, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		fmt.Printf("Failed: %v\n", err)
		return
	}

	start := time.Now()
	pipeline.SetState(gst.StatePlaying)

	for {
		info, _ := os.Stat(out)
		if info != nil && info.Size() > 0 {
			fmt.Printf("FIRST BYTE: %v\n", time.Since(start))
			break
		}
		if time.Since(start) > 20*time.Second {
			fmt.Println("TIMEOUT")
			break
		}
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
