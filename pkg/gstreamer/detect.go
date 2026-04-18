package gstreamer

import (
	"os/exec"
	"strings"
	"sync"

	"github.com/go-gst/go-gst/gst"
)

func Available() bool {
	_, err := exec.LookPath("gst-launch-1.0")
	return err == nil
}

var (
	pluginsOnce    sync.Once
	pluginsPresent bool
	decode10Once   sync.Once
	decode10Bit    bool
)

func pluginsAvailable() bool {
	pluginsOnce.Do(func() {
		pluginsPresent = gst.Find("tvproxydemux") != nil &&
			gst.Find("tvproxymux") != nil &&
			gst.Find("tvproxysrc") != nil
	})
	return pluginsPresent
}

func Decode10BitSupported() bool {
	decode10Once.Do(func() {
		for _, name := range []string{"vah265dec", "vah264dec", "vaav1dec", "nvh265dec", "nvh264dec", "nvav1dec", "qsvh265dec", "vtdec"} {
			el, _ := gst.NewElement(name)
			if el == nil {
				continue
			}
			for _, tmpl := range el.GetPadTemplates() {
				if tmpl.Direction() != gst.PadDirectionSource {
					continue
				}
				capsStr := tmpl.Caps().String()
				if strings.Contains(capsStr, "P010") || strings.Contains(capsStr, "10LE") || strings.Contains(capsStr, "10BE") {
					decode10Bit = true
					return
				}
			}
		}
	})
	return decode10Bit
}
