#include "gsttvproxymux.h"
#include <string.h>

GST_DEBUG_CATEGORY_STATIC (tvproxy_mux_debug);
#define GST_CAT_DEFAULT tvproxy_mux_debug

G_DEFINE_TYPE (GstTvproxyMux, gst_tvproxy_mux, GST_TYPE_BIN)

enum {
  PROP_0,
  PROP_OUTPUT_FORMAT,
  PROP_VIDEO_CODEC,
};

static GstStaticPadTemplate video_sink_template = GST_STATIC_PAD_TEMPLATE ("video",
    GST_PAD_SINK,
    GST_PAD_REQUEST,
    GST_STATIC_CAPS_ANY
    );

static GstStaticPadTemplate audio_sink_template = GST_STATIC_PAD_TEMPLATE ("audio",
    GST_PAD_SINK,
    GST_PAD_REQUEST,
    GST_STATIC_CAPS_ANY
    );

static GstStaticPadTemplate src_template = GST_STATIC_PAD_TEMPLATE ("src",
    GST_PAD_SRC,
    GST_PAD_ALWAYS,
    GST_STATIC_CAPS_ANY
    );

/* Forward declarations */
static GstPad *gst_tvproxy_mux_request_new_pad (GstElement *element,
    GstPadTemplate *templ, const gchar *name, const GstCaps *caps);
static void gst_tvproxy_mux_release_pad (GstElement *element, GstPad *pad);

static void
gst_tvproxy_mux_set_property (GObject *object, guint prop_id,
    const GValue *value, GParamSpec *pspec)
{
  GstTvproxyMux *self = GST_TVPROXY_MUX (object);

  switch (prop_id) {
    case PROP_OUTPUT_FORMAT:
      g_free (self->output_format);
      self->output_format = g_value_dup_string (value);
      break;
    case PROP_VIDEO_CODEC:
      g_free (self->video_codec);
      self->video_codec = g_value_dup_string (value);
      break;
    default:
      G_OBJECT_WARN_INVALID_PROPERTY_ID (object, prop_id, pspec);
      break;
  }
}

static void
gst_tvproxy_mux_get_property (GObject *object, guint prop_id,
    GValue *value, GParamSpec *pspec)
{
  GstTvproxyMux *self = GST_TVPROXY_MUX (object);

  switch (prop_id) {
    case PROP_OUTPUT_FORMAT:
      g_value_set_string (value, self->output_format);
      break;
    case PROP_VIDEO_CODEC:
      g_value_set_string (value, self->video_codec);
      break;
    default:
      G_OBJECT_WARN_INVALID_PROPERTY_ID (object, prop_id, pspec);
      break;
  }
}

static void
gst_tvproxy_mux_finalize (GObject *object)
{
  GstTvproxyMux *self = GST_TVPROXY_MUX (object);
  g_free (self->output_format);
  g_free (self->video_codec);
  G_OBJECT_CLASS (gst_tvproxy_mux_parent_class)->finalize (object);
}

/* Create the internal muxer based on output-format and add to bin.
 * Called lazily on first pad request so the property has been set. */
static gboolean
ensure_muxer (GstTvproxyMux *self)
{
  if (self->muxer)
    return TRUE;

  if (g_str_equal (self->output_format, "mpegts")) {
    self->muxer = gst_element_factory_make ("mpegtsmux", "muxer");
  } else {
    /* Default to mp4 */
    self->muxer = gst_element_factory_make ("mp4mux", "muxer");
    if (self->muxer) {
      g_object_set (self->muxer,
          "fragment-duration", (guint) 500,
          "streamable", TRUE,
          NULL);
    }
  }

  if (!self->muxer) {
    GST_ERROR_OBJECT (self, "Failed to create muxer for format '%s'",
        self->output_format);
    return FALSE;
  }

  gst_bin_add (GST_BIN (self), self->muxer);

  /* Link muxer src to our ghost src pad */
  GstPad *mux_src = gst_element_get_static_pad (self->muxer, "src");
  gst_ghost_pad_set_target (GST_GHOST_PAD (self->src_pad), mux_src);
  gst_object_unref (mux_src);

  GST_INFO_OBJECT (self, "Created %s muxer",
      g_str_equal (self->output_format, "mpegts") ? "mpegtsmux" : "mp4mux");

  return TRUE;
}

/* Helper: create the correct video parser from video-codec property, caps, or default */
static GstElement *
create_video_parser (GstTvproxyMux *self, const GstCaps *caps)
{
  const gchar *parser_factory = NULL;

  /* 1. Check video-codec property */
  if (self->video_codec && self->video_codec[0] != '\0') {
    if (g_str_equal (self->video_codec, "h264"))
      parser_factory = "h264parse";
    else if (g_str_equal (self->video_codec, "h265"))
      parser_factory = "h265parse";
    else if (g_str_equal (self->video_codec, "mpeg2"))
      parser_factory = "mpegvideoparse";
  }

  /* 2. Try caps hint from pad request */
  if (!parser_factory && caps && !gst_caps_is_any (caps) && !gst_caps_is_empty (caps)) {
    GstStructure *s = gst_caps_get_structure (caps, 0);
    const gchar *name = gst_structure_get_name (s);

    if (g_str_equal (name, "video/x-h265"))
      parser_factory = "h265parse";
    else if (g_str_equal (name, "video/mpeg"))
      parser_factory = "mpegvideoparse";
    else
      parser_factory = "h264parse";
  }

  /* 3. Default to h264parse */
  if (!parser_factory)
    parser_factory = "h264parse";

  GST_INFO_OBJECT (self, "Creating %s (config-interval=-1)", parser_factory);

  GstElement *parser = gst_element_factory_make (parser_factory, "vparse");
  if (parser && !g_str_equal (parser_factory, "mpegvideoparse"))
    g_object_set (parser, "config-interval", (gint) -1, NULL);

  return parser;
}

static GstPad *
gst_tvproxy_mux_request_new_pad (GstElement *element, GstPadTemplate *templ,
    const gchar *name, const GstCaps *caps)
{
  GstTvproxyMux *self = GST_TVPROXY_MUX (element);
  const gchar *templ_name = GST_PAD_TEMPLATE_NAME_TEMPLATE (templ);

  if (!ensure_muxer (self))
    return NULL;

  if (g_str_equal (templ_name, "video") && !self->video_pad) {
    /* Create video parser (h264parse by default, or from caps hint).
     * config-interval=-1 ensures SPS/PPS are repeated for mid-stream join. */
    self->video_parser = create_video_parser (self, caps);
    if (!self->video_parser) {
      GST_ERROR_OBJECT (self, "Failed to create video parser");
      return NULL;
    }

    gst_bin_add (GST_BIN (self), self->video_parser);
    gst_element_link (self->video_parser, self->muxer);
    gst_element_sync_state_with_parent (self->video_parser);

    /* Ghost pad exposes parser's sink */
    GstPad *parser_sink = gst_element_get_static_pad (self->video_parser, "sink");
    self->video_pad = gst_ghost_pad_new ("video", parser_sink);
    gst_object_unref (parser_sink);
    gst_pad_set_active (self->video_pad, TRUE);
    gst_element_add_pad (element, self->video_pad);

    GST_INFO_OBJECT (self, "Created video request pad with parser (config-interval=-1)");
    return self->video_pad;

  } else if (g_str_equal (templ_name, "audio") && !self->audio_pad) {
    /* Insert aacparse between ghost pad and muxer */
    self->audio_parser = gst_element_factory_make ("aacparse", "aparse");
    if (!self->audio_parser) {
      GST_ERROR_OBJECT (self, "Failed to create aacparse");
      return NULL;
    }
    gst_bin_add (GST_BIN (self), self->audio_parser);
    gst_element_link (self->audio_parser, self->muxer);
    gst_element_sync_state_with_parent (self->audio_parser);

    GstPad *parser_sink = gst_element_get_static_pad (self->audio_parser, "sink");
    self->audio_pad = gst_ghost_pad_new ("audio", parser_sink);
    gst_object_unref (parser_sink);
    gst_pad_set_active (self->audio_pad, TRUE);
    gst_element_add_pad (element, self->audio_pad);

    GST_INFO_OBJECT (self, "Created audio request pad");
    return self->audio_pad;
  }

  GST_WARNING_OBJECT (self, "Unknown pad template '%s'", templ_name);
  return NULL;
}

static void
gst_tvproxy_mux_release_pad (GstElement *element, GstPad *pad)
{
  GstTvproxyMux *self = GST_TVPROXY_MUX (element);

  if (pad == self->video_pad)
    self->video_pad = NULL;
  else if (pad == self->audio_pad)
    self->audio_pad = NULL;

  gst_element_remove_pad (element, pad);
}

static void
gst_tvproxy_mux_class_init (GstTvproxyMuxClass *klass)
{
  GObjectClass *gobject_class = G_OBJECT_CLASS (klass);
  GstElementClass *element_class = GST_ELEMENT_CLASS (klass);

  gobject_class->set_property = gst_tvproxy_mux_set_property;
  gobject_class->get_property = gst_tvproxy_mux_get_property;
  gobject_class->finalize = gst_tvproxy_mux_finalize;

  element_class->request_new_pad = gst_tvproxy_mux_request_new_pad;
  element_class->release_pad = gst_tvproxy_mux_release_pad;

  g_object_class_install_property (gobject_class, PROP_OUTPUT_FORMAT,
      g_param_spec_string ("output-format", "Output Format",
          "Muxer output format: 'mp4' (fragmented MP4) or 'mpegts' (MPEG-TS)",
          "mp4",
          G_PARAM_READWRITE | G_PARAM_STATIC_STRINGS));

  g_object_class_install_property (gobject_class, PROP_VIDEO_CODEC,
      g_param_spec_string ("video-codec", "Video Codec",
          "Video parser to use: 'h264', 'h265', 'mpeg2', or empty for auto (default h264)",
          "",
          G_PARAM_READWRITE | G_PARAM_STATIC_STRINGS));

  gst_element_class_add_static_pad_template (element_class, &video_sink_template);
  gst_element_class_add_static_pad_template (element_class, &audio_sink_template);
  gst_element_class_add_static_pad_template (element_class, &src_template);

  gst_element_class_set_static_metadata (element_class,
      "TV Proxy Muxer",
      "Codec/Muxer",
      "Muxes video and audio into MP4 or MPEG-TS with sensible defaults",
      "tvproxy");

  GST_DEBUG_CATEGORY_INIT (tvproxy_mux_debug, "tvproxymux", 0,
      "TV Proxy Muxer");
}

static void
gst_tvproxy_mux_init (GstTvproxyMux *self)
{
  self->output_format = g_strdup ("mp4");
  self->video_codec = g_strdup ("");
  self->muxer = NULL;
  self->video_parser = NULL;
  self->audio_parser = NULL;
  self->video_pad = NULL;
  self->audio_pad = NULL;

  /* Create ghost src pad with no target yet — set when muxer is created */
  self->src_pad = gst_ghost_pad_new_no_target ("src", GST_PAD_SRC);
  gst_element_add_pad (GST_ELEMENT (self), self->src_pad);
}
