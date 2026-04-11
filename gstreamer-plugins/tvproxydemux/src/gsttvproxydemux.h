#ifndef __GST_TVPROXY_DEMUX_H__
#define __GST_TVPROXY_DEMUX_H__

#include <gst/gst.h>

G_BEGIN_DECLS

#define GST_TYPE_TVPROXY_DEMUX (gst_tvproxy_demux_get_type())
G_DECLARE_FINAL_TYPE (GstTvproxyDemux, gst_tvproxy_demux, GST, TVPROXY_DEMUX, GstBin)

/* Pending audio pad info for language selection */
typedef struct {
  GstPad *pad;
  GstCaps *caps;
  gchar *language;       /* ISO 639 language code from tags, or NULL */
  gboolean is_audio_desc; /* TRUE if this is an audio description track */
} PendingAudioPad;

struct _GstTvproxyDemux {
  GstBin parent;

  /* Internal fixed elements (created at init) */
  GstElement *tsparse;
  GstElement *tsdemux;
  GstElement *video_queue;
  GstElement *audio_queue;

  /* Ghost pads */
  GstPad *sink_pad;
  GstPad *video_pad;
  GstPad *audio_pad;

  /* Dynamic elements (created in pad-added, stored for cleanup) */
  GList *dynamic_elements;

  /* Pending audio pads for language selection */
  GList *pending_audio_pads;  /* list of PendingAudioPad* */

  /* State */
  gboolean video_linked;
  gboolean audio_linked;
  gboolean video_interlaced;

  /* Properties */
  gint audio_channels;
  gchar *audio_codec;       /* "aac" or "copy" */
  gchar *video_codec_hint;  /* e.g. "h264", "h265", "mpeg2" or "" for auto */
  gchar *audio_language;    /* e.g. "eng", "fra" or "" for first available */
};

G_END_DECLS

#endif /* __GST_TVPROXY_DEMUX_H__ */
