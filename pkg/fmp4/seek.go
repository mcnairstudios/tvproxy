package fmp4

/*
#cgo pkg-config: gstreamer-1.0
#include <gst/gst.h>

static gboolean do_seek(GstElement *element, gint64 pos_ns) {
    return gst_element_seek_simple(element,
        GST_FORMAT_TIME,
        GST_SEEK_FLAG_FLUSH | GST_SEEK_FLAG_KEY_UNIT,
        pos_ns);
}
*/
import "C"
import (
	"unsafe"

	"github.com/go-gst/go-gst/gst"
)

func SeekPipeline(pipeline *gst.Pipeline, positionNs int64) bool {
	cPipeline := (*C.GstElement)(unsafe.Pointer(pipeline.Unsafe()))
	ok := C.do_seek(cPipeline, C.gint64(positionNs))
	return ok != 0
}
