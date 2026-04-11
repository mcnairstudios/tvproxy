#include "gsttvproxydemux.h"

static gboolean
plugin_init (GstPlugin *plugin)
{
  return gst_element_register (plugin, "tvproxydemux",
      GST_RANK_NONE, GST_TYPE_TVPROXY_DEMUX);
}

GST_PLUGIN_DEFINE (
    GST_VERSION_MAJOR,
    GST_VERSION_MINOR,
    tvproxydemux,
    "MPEG-TS demux with auto audio transcode for tvproxy",
    plugin_init,
    "0.1.0",
    "LGPL",
    "tvproxy",
    "https://github.com/gavinmcnair/tvproxy"
)
