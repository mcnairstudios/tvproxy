#ifndef __GST_TVPROXY_SRC_H__
#define __GST_TVPROXY_SRC_H__

#include <gst/gst.h>

G_BEGIN_DECLS

#define GST_TYPE_TVPROXY_SRC (gst_tvproxy_src_get_type())
G_DECLARE_FINAL_TYPE (GstTvproxySrc, gst_tvproxy_src, GST, TVPROXY_SRC, GstBin)

struct _GstTvproxySrc {
  GstBin parent;

  /* Internal elements — which ones are created depends on location scheme */
  GstElement *source;    /* souphttpsrc, rtspsrc, or filesrc */
  GstElement *depay;     /* rtpmp2tdepay (RTSP mode only) */

  GstPad *src_pad;

  /* Properties */
  gchar *location;
  gboolean is_live;
  gchar *rtsp_transport; /* "tcp" or "udp" */

  /* State */
  gboolean elements_configured;
};

G_END_DECLS

#endif /* __GST_TVPROXY_SRC_H__ */
