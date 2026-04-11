#ifndef __GST_TVPROXY_MUX_H__
#define __GST_TVPROXY_MUX_H__

#include <gst/gst.h>

G_BEGIN_DECLS

#define GST_TYPE_TVPROXY_MUX (gst_tvproxy_mux_get_type())
G_DECLARE_FINAL_TYPE (GstTvproxyMux, gst_tvproxy_mux, GST, TVPROXY_MUX, GstBin)

struct _GstTvproxyMux {
  GstBin parent;

  GstElement *muxer;
  GstElement *video_parser;  /* h264parse/h265parse/mpegvideoparse */
  GstElement *audio_parser;  /* aacparse */
  GstPad *src_pad;
  GstPad *video_pad;  /* ghost request pad -> parser -> muxer */
  GstPad *audio_pad;  /* ghost request pad -> parser -> muxer */

  gchar *output_format; /* "mp4" or "mpegts" */
  gchar *video_codec;   /* "h264", "h265", "mpeg2", or "" for auto (h264 default) */
};

G_END_DECLS

#endif /* __GST_TVPROXY_MUX_H__ */
