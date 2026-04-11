#include "gsttvproxydemux.h"
#include "gsttvproxysrc.h"
#include "gsttvproxymux.h"

static gboolean
plugin_init (GstPlugin *plugin)
{
  gboolean ok = TRUE;

  ok &= gst_element_register (plugin, "tvproxydemux",
      GST_RANK_NONE, GST_TYPE_TVPROXY_DEMUX);
  ok &= gst_element_register (plugin, "tvproxysrc",
      GST_RANK_NONE, GST_TYPE_TVPROXY_SRC);
  ok &= gst_element_register (plugin, "tvproxymux",
      GST_RANK_NONE, GST_TYPE_TVPROXY_MUX);

  return ok;
}

GST_PLUGIN_DEFINE (
    GST_VERSION_MAJOR,
    GST_VERSION_MINOR,
    tvproxydemux,
    "MPEG-TS source, demux, and mux elements for tvproxy",
    plugin_init,
    "0.1.0",
    "LGPL",
    "tvproxy",
    "https://github.com/gavinmcnair/tvproxy"
)
