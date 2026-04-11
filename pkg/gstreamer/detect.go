package gstreamer

import (
	"os/exec"
	"sync"

	"github.com/go-gst/go-gst/gst"
)

func Available() bool {
	_, err := exec.LookPath("gst-launch-1.0")
	return err == nil
}

func DiscovererAvailable() bool {
	_, err := exec.LookPath("gst-discoverer-1.0")
	return err == nil
}

var (
	pluginsOnce    sync.Once
	pluginsPresent bool
)

func PluginsAvailable() bool {
	pluginsOnce.Do(func() {
		pluginsPresent = gst.Find("tvproxydemux") != nil &&
			gst.Find("tvproxymux") != nil &&
			gst.Find("tvproxysrc") != nil
	})
	return pluginsPresent
}
