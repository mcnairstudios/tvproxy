package gstreamer

import (
	"os/exec"
)

func Available() bool {
	_, err := exec.LookPath("gst-launch-1.0")
	return err == nil
}

func DiscovererAvailable() bool {
	_, err := exec.LookPath("gst-discoverer-1.0")
	return err == nil
}
