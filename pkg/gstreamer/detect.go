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
		reg := gst.GetRegistry()
		demux, _ := reg.FindPlugin("tvproxydemux")
		mux, _ := reg.FindPlugin("tvproxymux")
		src, _ := reg.FindPlugin("tvproxysrc")
		pluginsPresent = demux != nil && mux != nil && src != nil
	})
	return pluginsPresent
}
