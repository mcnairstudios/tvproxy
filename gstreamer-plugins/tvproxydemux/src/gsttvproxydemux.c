#include "gsttvproxydemux.h"
#include <string.h>

GST_DEBUG_CATEGORY_STATIC (tvproxy_demux_debug);
#define GST_CAT_DEFAULT tvproxy_demux_debug

G_DEFINE_TYPE (GstTvproxyDemux, gst_tvproxy_demux, GST_TYPE_BIN)

enum {
  PROP_0,
  PROP_AUDIO_CHANNELS,
  PROP_AUDIO_CODEC,
  PROP_VIDEO_CODEC_HINT,
  PROP_AUDIO_LANGUAGE,
  PROP_VIDEO_INTERLACED,
};

/* Pad templates */
static GstStaticPadTemplate sink_template = GST_STATIC_PAD_TEMPLATE ("sink",
    GST_PAD_SINK,
    GST_PAD_ALWAYS,
    GST_STATIC_CAPS ("video/mpegts")
    );

static GstStaticPadTemplate video_src_template = GST_STATIC_PAD_TEMPLATE ("video",
    GST_PAD_SRC,
    GST_PAD_ALWAYS,
    GST_STATIC_CAPS ("video/x-h264; video/x-h265; video/mpeg, mpegversion=(int)2")
    );

static GstStaticPadTemplate audio_src_template = GST_STATIC_PAD_TEMPLATE ("audio",
    GST_PAD_SRC,
    GST_PAD_ALWAYS,
    GST_STATIC_CAPS ("audio/mpeg, mpegversion=(int)4")
    );

/* Forward declarations */
static void gst_tvproxy_demux_set_property (GObject *object, guint prop_id,
    const GValue *value, GParamSpec *pspec);
static void gst_tvproxy_demux_get_property (GObject *object, guint prop_id,
    GValue *value, GParamSpec *pspec);
static void gst_tvproxy_demux_dispose (GObject *object);
static void gst_tvproxy_demux_finalize (GObject *object);
static void on_pad_added (GstElement *tsdemux, GstPad *pad, GstTvproxyDemux *self);
static void on_no_more_pads (GstElement *tsdemux, GstTvproxyDemux *self);
static GstStateChangeReturn gst_tvproxy_demux_change_state (GstElement *element,
    GstStateChange transition);
static void gst_tvproxy_demux_handle_message (GstBin *bin, GstMessage *message);

/* ---- Helpers ---- */

static void
pending_audio_pad_free (PendingAudioPad *p)
{
  if (p->pad)
    gst_object_unref (p->pad);
  if (p->caps)
    gst_caps_unref (p->caps);
  g_free (p->language);
  g_free (p);
}

static void
clear_pending_audio_pads (GstTvproxyDemux *self)
{
  g_list_free_full (self->pending_audio_pads, (GDestroyNotify) pending_audio_pad_free);
  self->pending_audio_pads = NULL;
}

/* Create element, add to bin, track in dynamic list */
static GstElement *
make_and_add (GstTvproxyDemux *self, const gchar *factory, const gchar *name)
{
  GstElement *elem = gst_element_factory_make (factory, name);
  if (!elem) {
    GST_ERROR_OBJECT (self, "Failed to create element '%s'", factory);
    return NULL;
  }
  gst_bin_add (GST_BIN (self), elem);
  self->dynamic_elements = g_list_append (self->dynamic_elements, elem);
  return elem;
}

/* Try to extract language code from a pad's sticky TAG event */
static gchar *
get_pad_language (GstPad *pad)
{
  GstEvent *tag_event = gst_pad_get_sticky_event (pad, GST_EVENT_TAG, 0);
  if (!tag_event)
    return NULL;

  GstTagList *tags = NULL;
  gst_event_parse_tag (tag_event, &tags);

  gchar *lang = NULL;
  if (tags)
    gst_tag_list_get_string (tags, GST_TAG_LANGUAGE_CODE, &lang);

  gst_event_unref (tag_event);
  return lang;
}

/* Check if an audio pad is an audio description (AD) track.
 * AD tracks in DVB are typically signalled via component descriptors in the PMT.
 * tsdemux exposes this in tags or the pad name. We check for:
 * - "audio-description" in tags
 * - "visual-impaired" tag
 * - Pad name heuristics (some tsdemux versions mark AD streams)
 */
static gboolean
is_audio_description (GstPad *pad)
{
  GstEvent *tag_event = gst_pad_get_sticky_event (pad, GST_EVENT_TAG, 0);
  if (!tag_event)
    return FALSE;

  GstTagList *tags = NULL;
  gst_event_parse_tag (tag_event, &tags);

  gboolean is_ad = FALSE;
  if (tags) {
    /* Check for audio type tag — DVB marks AD as "visual-impaired-commentary" */
    gchar *audio_type = NULL;
    if (gst_tag_list_get_string (tags, "audio-type", &audio_type)) {
      if (audio_type && strstr (audio_type, "visual-impaired"))
        is_ad = TRUE;
      g_free (audio_type);
    }

    /* Some implementations use a private tag */
    guint component_tag = 0;
    if (gst_tag_list_get_uint (tags, "component-tag", &component_tag)) {
      /* DVB component_type 0x03 = audio description for visually impaired */
      if (component_tag == 0x03)
        is_ad = TRUE;
    }
  }

  gst_event_unref (tag_event);
  return is_ad;
}

/* ---- Video chain ---- */

static void
build_video_chain (GstTvproxyDemux *self, GstPad *demux_pad, GstStructure *s)
{
  const gchar *name = gst_structure_get_name (s);
  const gchar *parser_factory = NULL;

  /* Check for video-codec-hint override */
  if (self->video_codec_hint && self->video_codec_hint[0] != '\0') {
    if (g_str_equal (self->video_codec_hint, "h264"))
      parser_factory = "h264parse";
    else if (g_str_equal (self->video_codec_hint, "h265"))
      parser_factory = "h265parse";
    else if (g_str_equal (self->video_codec_hint, "mpeg2"))
      parser_factory = "mpegvideoparse";
    else
      GST_WARNING_OBJECT (self, "Unknown video-codec-hint '%s', falling back to auto",
          self->video_codec_hint);
  }

  /* Auto-detect from caps if no hint or unknown hint */
  if (!parser_factory) {
    if (g_str_equal (name, "video/x-h264")) {
      parser_factory = "h264parse";
    } else if (g_str_equal (name, "video/x-h265")) {
      parser_factory = "h265parse";
    } else if (g_str_equal (name, "video/mpeg")) {
      parser_factory = "mpegvideoparse";
    } else {
      GST_WARNING_OBJECT (self, "Unknown video caps: %s", name);
      return;
    }
  }

  /* Detect interlaced content from caps */
  const gchar *interlace_mode = gst_structure_get_string (s, "interlace-mode");
  if (interlace_mode && !g_str_equal (interlace_mode, "progressive")) {
    self->video_interlaced = TRUE;
    GST_INFO_OBJECT (self, "Video is interlaced (mode: %s)", interlace_mode);
  } else {
    self->video_interlaced = FALSE;
  }

  GST_INFO_OBJECT (self, "Building video chain with %s", parser_factory);

  GstElement *parser = make_and_add (self, parser_factory, "video_parser");
  if (!parser)
    return;

  /* Link: demux pad -> parser -> video_queue */
  gst_element_link (parser, self->video_queue);
  gst_element_sync_state_with_parent (parser);

  GstPad *parser_sink = gst_element_get_static_pad (parser, "sink");
  GstPadLinkReturn ret = gst_pad_link (demux_pad, parser_sink);
  gst_object_unref (parser_sink);

  if (ret != GST_PAD_LINK_OK) {
    GST_ERROR_OBJECT (self, "Failed to link video pad: %d", ret);
  } else {
    GST_INFO_OBJECT (self, "Video chain linked successfully (interlaced=%d)",
        self->video_interlaced);
  }
}

/* ---- Audio chain ---- */

static void
build_audio_chain (GstTvproxyDemux *self, GstPad *demux_pad, GstStructure *s)
{
  const gchar *name = gst_structure_get_name (s);
  gint mpegversion = 0;
  const gchar *stream_format = NULL;

  gst_structure_get_int (s, "mpegversion", &mpegversion);
  stream_format = gst_structure_get_string (s, "stream-format");

  GST_INFO_OBJECT (self, "Audio caps: %s, mpegversion=%d, stream-format=%s",
      name, mpegversion, stream_format ? stream_format : "(null)");

  /* If audio-codec=copy, pass through plain AAC only.
   * AAC-LATM, AC3, MP2 etc. still need transcoding because muxers
   * can't accept LOAS format or non-AAC audio directly. */
  if (self->audio_codec && g_str_equal (self->audio_codec, "copy")) {
    if (mpegversion == 4 && stream_format &&
        (g_str_equal (stream_format, "raw") || g_str_equal (stream_format, "adts"))) {
      /* Plain AAC — true passthrough */
      GST_INFO_OBJECT (self, "Audio copy mode — plain AAC passthrough");

      GstElement *aacparse = make_and_add (self, "aacparse", "audio_aacparse");
      if (!aacparse)
        return;

      gst_element_link (aacparse, self->audio_queue);
      gst_element_sync_state_with_parent (aacparse);

      GstPad *sink = gst_element_get_static_pad (aacparse, "sink");
      GstPadLinkReturn ret = gst_pad_link (demux_pad, sink);
      gst_object_unref (sink);

      if (ret != GST_PAD_LINK_OK)
        GST_ERROR_OBJECT (self, "Failed to link audio copy pad: %d", ret);
      else
        GST_INFO_OBJECT (self, "Audio copy chain linked successfully");
      return;
    }
    GST_INFO_OBJECT (self, "Audio copy mode requested but stream needs transcoding (%s), falling back",
        stream_format ? stream_format : name);
    /* Fall through to transcode path */
  }

  /* Transcode mode: decode to raw, re-encode to AAC */
  GstElement *first = NULL;
  GstElement *last = NULL;

  if (mpegversion == 4 && stream_format &&
      (g_str_equal (stream_format, "loas") || g_str_equal (stream_format, "loas/latm"))) {
    /* AAC-LATM: aacparse ! avdec_aac_latm ! audioconvert ! audioresample ! capsfilter ! faac ! aacparse */
    GST_INFO_OBJECT (self, "Building AAC-LATM audio chain");

    GstElement *aacparse_in = make_and_add (self, "aacparse", "audio_aacparse_in");
    GstElement *decoder = make_and_add (self, "avdec_aac_latm", "audio_decoder");
    GstElement *convert = make_and_add (self, "audioconvert", "audio_convert");
    GstElement *resample = make_and_add (self, "audioresample", "audio_resample");
    GstElement *capsfilter = make_and_add (self, "capsfilter", "audio_capsfilter");
    GstElement *encoder = make_and_add (self, "faac", "audio_encoder");
    GstElement *aacparse_out = make_and_add (self, "aacparse", "audio_aacparse_out");

    if (!aacparse_in || !decoder || !convert || !resample || !capsfilter || !encoder || !aacparse_out)
      return;

    gchar *caps_str = g_strdup_printf ("audio/x-raw,channels=%d", self->audio_channels);
    GstCaps *raw_caps = gst_caps_from_string (caps_str);
    g_object_set (capsfilter, "caps", raw_caps, NULL);
    gst_caps_unref (raw_caps);
    g_free (caps_str);

    gst_element_link_many (aacparse_in, decoder, convert, resample, capsfilter,
        encoder, aacparse_out, NULL);

    first = aacparse_in;
    last = aacparse_out;

  } else if (mpegversion == 4) {
    /* Plain AAC: aacparse passthrough */
    GST_INFO_OBJECT (self, "Building plain AAC passthrough chain");

    GstElement *aacparse = make_and_add (self, "aacparse", "audio_aacparse");
    if (!aacparse)
      return;

    first = aacparse;
    last = aacparse;

  } else if (g_str_equal (name, "audio/x-ac3") || g_str_equal (name, "audio/x-eac3")) {
    /* AC3/EAC3 */
    GST_INFO_OBJECT (self, "Building AC3 audio chain");

    const gchar *dec_factory = g_str_equal (name, "audio/x-eac3") ? "avdec_eac3" : "avdec_ac3";
    GstElement *decoder = make_and_add (self, dec_factory, "audio_decoder");
    GstElement *convert = make_and_add (self, "audioconvert", "audio_convert");
    GstElement *resample = make_and_add (self, "audioresample", "audio_resample");
    GstElement *capsfilter = make_and_add (self, "capsfilter", "audio_capsfilter");
    GstElement *encoder = make_and_add (self, "faac", "audio_encoder");
    GstElement *aacparse_out = make_and_add (self, "aacparse", "audio_aacparse_out");

    if (!decoder || !convert || !resample || !capsfilter || !encoder || !aacparse_out)
      return;

    gchar *caps_str = g_strdup_printf ("audio/x-raw,channels=%d", self->audio_channels);
    GstCaps *raw_caps = gst_caps_from_string (caps_str);
    g_object_set (capsfilter, "caps", raw_caps, NULL);
    gst_caps_unref (raw_caps);
    g_free (caps_str);

    gst_element_link_many (decoder, convert, resample, capsfilter, encoder, aacparse_out, NULL);

    first = decoder;
    last = aacparse_out;

  } else if (mpegversion == 1) {
    /* MP2: mpegaudioparse ! mpg123audiodec ! transcode
     * mpegaudioparse is required before mpg123audiodec (same pattern as aacparse before avdec_aac_latm) */
    GST_INFO_OBJECT (self, "Building MP2 audio chain");

    GstElement *parser = make_and_add (self, "mpegaudioparse", "audio_mpegparse");
    GstElement *decoder = make_and_add (self, "mpg123audiodec", "audio_decoder");
    GstElement *convert = make_and_add (self, "audioconvert", "audio_convert");
    GstElement *resample = make_and_add (self, "audioresample", "audio_resample");
    GstElement *capsfilter = make_and_add (self, "capsfilter", "audio_capsfilter");
    GstElement *encoder = make_and_add (self, "faac", "audio_encoder");
    GstElement *aacparse_out = make_and_add (self, "aacparse", "audio_aacparse_out");

    if (!parser || !decoder || !convert || !resample || !capsfilter || !encoder || !aacparse_out)
      return;

    gchar *caps_str = g_strdup_printf ("audio/x-raw,channels=%d", self->audio_channels);
    GstCaps *raw_caps = gst_caps_from_string (caps_str);
    g_object_set (capsfilter, "caps", raw_caps, NULL);
    gst_caps_unref (raw_caps);
    g_free (caps_str);

    gst_element_link_many (parser, decoder, convert, resample, capsfilter, encoder, aacparse_out, NULL);

    first = parser;
    last = aacparse_out;

  } else {
    GST_WARNING_OBJECT (self, "Unknown audio type: %s mpegversion=%d", name, mpegversion);
    return;
  }

  /* Link chain -> audio_queue, sync states, connect demux pad */
  gst_element_link (last, self->audio_queue);

  for (GList *l = self->dynamic_elements; l; l = l->next) {
    GstElement *elem = GST_ELEMENT (l->data);
    GstState state;
    gst_element_get_state (elem, &state, NULL, 0);
    if (state == GST_STATE_VOID_PENDING || state == GST_STATE_NULL)
      gst_element_sync_state_with_parent (elem);
  }

  GstPad *first_sink = gst_element_get_static_pad (first, "sink");
  GstPadLinkReturn ret = gst_pad_link (demux_pad, first_sink);
  gst_object_unref (first_sink);

  if (ret != GST_PAD_LINK_OK) {
    GST_ERROR_OBJECT (self, "Failed to link audio pad: %d", ret);
  } else {
    GST_INFO_OBJECT (self, "Audio chain linked successfully");
  }
}

/* ---- Audio language selection ---- */

/* Select the best audio pad from pending list and link it */
static void
select_and_link_audio (GstTvproxyDemux *self)
{
  if (self->audio_linked || !self->pending_audio_pads)
    return;

  PendingAudioPad *best = NULL;
  PendingAudioPad *first_non_ad = NULL;

  for (GList *l = self->pending_audio_pads; l; l = l->next) {
    PendingAudioPad *p = l->data;

    /* Skip audio description tracks */
    if (p->is_audio_desc) {
      GST_INFO_OBJECT (self, "Skipping audio description track (lang=%s)",
          p->language ? p->language : "unknown");
      continue;
    }

    /* Remember first non-AD track as fallback */
    if (!first_non_ad)
      first_non_ad = p;

    /* If language preference is set, check for match */
    if (self->audio_language && self->audio_language[0] != '\0' && p->language) {
      if (g_str_equal (p->language, self->audio_language)) {
        best = p;
        GST_INFO_OBJECT (self, "Found preferred audio language: %s", p->language);
        break;
      }
    }
  }

  /* Fall back to first non-AD track if no language match */
  if (!best)
    best = first_non_ad;

  /* Last resort: take literally the first pad (even AD) */
  if (!best && self->pending_audio_pads)
    best = self->pending_audio_pads->data;

  if (!best) {
    GST_WARNING_OBJECT (self, "No audio pad available to link");
    return;
  }

  GST_INFO_OBJECT (self, "Selected audio pad (lang=%s, ad=%d)",
      best->language ? best->language : "unknown", best->is_audio_desc);

  GstStructure *s = gst_caps_get_structure (best->caps, 0);
  build_audio_chain (self, best->pad, s);
  self->audio_linked = TRUE;

  clear_pending_audio_pads (self);
}

/* ---- Signal handlers ---- */

static void
on_pad_added (GstElement *tsdemux, GstPad *pad, GstTvproxyDemux *self)
{
  GstCaps *caps = gst_pad_get_current_caps (pad);
  if (!caps)
    caps = gst_pad_query_caps (pad, NULL);
  if (!caps)
    return;

  GstStructure *s = gst_caps_get_structure (caps, 0);
  const gchar *name = gst_structure_get_name (s);

  GST_DEBUG_OBJECT (self, "tsdemux pad-added: %s", gst_caps_to_string (caps));

  if (g_str_has_prefix (name, "video/") && !self->video_linked) {
    build_video_chain (self, pad, s);
    self->video_linked = TRUE;

  } else if ((g_str_has_prefix (name, "audio/") ||
              strstr (name, "audio") != NULL) && !self->audio_linked) {

    /* If no language preference set, take first non-AD audio pad immediately */
    if (!self->audio_language || self->audio_language[0] == '\0') {
      gboolean is_ad = is_audio_description (pad);
      if (is_ad && !self->audio_linked) {
        GST_INFO_OBJECT (self, "Skipping audio description track (no language pref, waiting for non-AD)");
        /* Store it in case it's the only one */
        PendingAudioPad *p = g_new0 (PendingAudioPad, 1);
        p->pad = gst_object_ref (pad);
        p->caps = gst_caps_ref (caps);
        p->language = get_pad_language (pad);
        p->is_audio_desc = TRUE;
        self->pending_audio_pads = g_list_append (self->pending_audio_pads, p);
      } else {
        build_audio_chain (self, pad, s);
        self->audio_linked = TRUE;
        clear_pending_audio_pads (self);
      }
    } else {
      /* Language preference set — collect all audio pads, decide in no-more-pads */
      PendingAudioPad *p = g_new0 (PendingAudioPad, 1);
      p->pad = gst_object_ref (pad);
      p->caps = gst_caps_ref (caps);
      p->language = get_pad_language (pad);
      p->is_audio_desc = is_audio_description (pad);
      self->pending_audio_pads = g_list_append (self->pending_audio_pads, p);

      GST_INFO_OBJECT (self, "Queued audio pad (lang=%s, ad=%d) for language selection",
          p->language ? p->language : "unknown", p->is_audio_desc);
    }
  } else {
    /* Unhandled pad (second audio, subtitle, etc.) — attach a fakesink
     * to prevent not-negotiated flow errors from killing the pipeline */
    static gint fakesink_count = 0;
    gchar *fs_name = g_strdup_printf ("fakesink_unused_%d", g_atomic_int_add (&fakesink_count, 1));
    GstElement *fs = make_and_add (self, "fakesink", fs_name);
    g_free (fs_name);
    if (fs) {
      g_object_set (fs, "sync", FALSE, "async", FALSE, NULL);
      gst_element_sync_state_with_parent (fs);
      GstPad *fs_sink = gst_element_get_static_pad (fs, "sink");
      gst_pad_link (pad, fs_sink);
      gst_object_unref (fs_sink);
      GST_DEBUG_OBJECT (self, "Attached fakesink to unused pad: %s", name);
    }
  }

  gst_caps_unref (caps);
}

static void
on_no_more_pads (GstElement *tsdemux, GstTvproxyDemux *self)
{
  GST_INFO_OBJECT (self, "tsdemux: no-more-pads");

  /* If audio wasn't linked yet (language selection deferred, or only AD tracks seen),
   * pick the best available now */
  if (!self->audio_linked && self->pending_audio_pads) {
    select_and_link_audio (self);
  }
}

static GstStateChangeReturn
gst_tvproxy_demux_change_state (GstElement *element, GstStateChange transition)
{
  GstTvproxyDemux *self = GST_TVPROXY_DEMUX (element);

  switch (transition) {
    case GST_STATE_CHANGE_READY_TO_NULL:
      self->video_linked = FALSE;
      self->audio_linked = FALSE;
      self->video_interlaced = FALSE;
      clear_pending_audio_pads (self);
      break;
    default:
      break;
  }

  return GST_ELEMENT_CLASS (gst_tvproxy_demux_parent_class)->change_state (element, transition);
}

/* Suppress not-negotiated errors from internal tsdemux for unlinked pads
 * (e.g. second audio stream, subtitle stream). These are expected — we only
 * link one video and one audio pad, the rest are intentionally unlinked. */
static void
gst_tvproxy_demux_handle_message (GstBin *bin, GstMessage *message)
{
  if (GST_MESSAGE_TYPE (message) == GST_MESSAGE_ERROR) {
    GError *err = NULL;
    gst_message_parse_error (message, &err, NULL);

    if (err && err->code == GST_STREAM_ERROR_FAILED &&
        strstr (err->message, "not-negotiated") != NULL) {
      GST_DEBUG_OBJECT (bin, "Suppressing not-negotiated error from internal element");
      g_error_free (err);
      gst_message_unref (message);
      return;
    }
    if (err)
      g_error_free (err);
  }

  GST_BIN_CLASS (gst_tvproxy_demux_parent_class)->handle_message (bin, message);
}

/* ---- GObject boilerplate ---- */

static void
gst_tvproxy_demux_class_init (GstTvproxyDemuxClass *klass)
{
  GObjectClass *gobject_class = G_OBJECT_CLASS (klass);
  GstElementClass *element_class = GST_ELEMENT_CLASS (klass);
  GstBinClass *bin_class = GST_BIN_CLASS (klass);

  gobject_class->set_property = gst_tvproxy_demux_set_property;
  gobject_class->get_property = gst_tvproxy_demux_get_property;
  gobject_class->dispose = gst_tvproxy_demux_dispose;
  gobject_class->finalize = gst_tvproxy_demux_finalize;

  bin_class->handle_message = gst_tvproxy_demux_handle_message;

  element_class->change_state = gst_tvproxy_demux_change_state;

  g_object_class_install_property (gobject_class, PROP_AUDIO_CHANNELS,
      g_param_spec_int ("audio-channels", "Audio Channels",
          "Target number of audio channels", 1, 8, 2,
          G_PARAM_READWRITE | G_PARAM_STATIC_STRINGS));

  g_object_class_install_property (gobject_class, PROP_AUDIO_CODEC,
      g_param_spec_string ("audio-codec", "Audio Codec",
          "Audio output mode: 'aac' (transcode to AAC) or 'copy' (passthrough)",
          "aac",
          G_PARAM_READWRITE | G_PARAM_STATIC_STRINGS));

  g_object_class_install_property (gobject_class, PROP_VIDEO_CODEC_HINT,
      g_param_spec_string ("video-codec-hint", "Video Codec Hint",
          "Force video parser: 'h264', 'h265', 'mpeg2', or empty for auto-detect",
          "",
          G_PARAM_READWRITE | G_PARAM_STATIC_STRINGS));

  g_object_class_install_property (gobject_class, PROP_AUDIO_LANGUAGE,
      g_param_spec_string ("audio-language", "Audio Language",
          "Preferred audio language as ISO 639 code (e.g. 'eng', 'fra'). "
          "Empty string selects first non-AD track.",
          "",
          G_PARAM_READWRITE | G_PARAM_STATIC_STRINGS));

  g_object_class_install_property (gobject_class, PROP_VIDEO_INTERLACED,
      g_param_spec_boolean ("video-interlaced", "Video Interlaced",
          "TRUE if the source video is interlaced (read-only, detected from stream)",
          FALSE,
          G_PARAM_READABLE | G_PARAM_STATIC_STRINGS));

  gst_element_class_add_static_pad_template (element_class, &sink_template);
  gst_element_class_add_static_pad_template (element_class, &video_src_template);
  gst_element_class_add_static_pad_template (element_class, &audio_src_template);

  gst_element_class_set_static_metadata (element_class,
      "TV Proxy MPEG-TS Demuxer",
      "Codec/Demuxer/Audio/Video",
      "Demuxes MPEG-TS and transcodes audio to AAC with static pads",
      "tvproxy");

  GST_DEBUG_CATEGORY_INIT (tvproxy_demux_debug, "tvproxydemux", 0,
      "TV Proxy MPEG-TS Demuxer");
}

static void
gst_tvproxy_demux_init (GstTvproxyDemux *self)
{
  GstPad *target;

  self->audio_channels = 2;
  self->audio_codec = g_strdup ("aac");
  self->video_codec_hint = g_strdup ("");
  self->audio_language = g_strdup ("");
  self->video_linked = FALSE;
  self->audio_linked = FALSE;
  self->video_interlaced = FALSE;
  self->dynamic_elements = NULL;
  self->pending_audio_pads = NULL;

  /* Create fixed internal elements */
  self->tsparse = gst_element_factory_make ("tsparse", "tsparse");
  g_object_set (self->tsparse, "set-timestamps", TRUE, NULL);

  self->tsdemux = gst_element_factory_make ("tsdemux", "tsdemux");

  self->video_queue = gst_element_factory_make ("queue", "video_queue");
  self->audio_queue = gst_element_factory_make ("queue", "audio_queue");

  /* Add all to bin */
  gst_bin_add_many (GST_BIN (self),
      self->tsparse, self->tsdemux,
      self->video_queue, self->audio_queue, NULL);

  /* Link tsparse -> tsdemux */
  gst_element_link (self->tsparse, self->tsdemux);

  /* Ghost sink pad -> tsparse sink */
  target = gst_element_get_static_pad (self->tsparse, "sink");
  self->sink_pad = gst_ghost_pad_new ("sink", target);
  gst_element_add_pad (GST_ELEMENT (self), self->sink_pad);
  gst_object_unref (target);

  /* Ghost video pad -> video_queue src */
  target = gst_element_get_static_pad (self->video_queue, "src");
  self->video_pad = gst_ghost_pad_new ("video", target);
  gst_element_add_pad (GST_ELEMENT (self), self->video_pad);
  gst_object_unref (target);

  /* Ghost audio pad -> audio_queue src */
  target = gst_element_get_static_pad (self->audio_queue, "src");
  self->audio_pad = gst_ghost_pad_new ("audio", target);
  gst_element_add_pad (GST_ELEMENT (self), self->audio_pad);
  gst_object_unref (target);

  /* Connect signals on tsdemux */
  g_signal_connect (self->tsdemux, "pad-added", G_CALLBACK (on_pad_added), self);
  g_signal_connect (self->tsdemux, "no-more-pads", G_CALLBACK (on_no_more_pads), self);
}

static void
gst_tvproxy_demux_dispose (GObject *object)
{
  GstTvproxyDemux *self = GST_TVPROXY_DEMUX (object);

  clear_pending_audio_pads (self);
  g_list_free (self->dynamic_elements);
  self->dynamic_elements = NULL;

  G_OBJECT_CLASS (gst_tvproxy_demux_parent_class)->dispose (object);
}

static void
gst_tvproxy_demux_finalize (GObject *object)
{
  GstTvproxyDemux *self = GST_TVPROXY_DEMUX (object);

  g_free (self->audio_codec);
  g_free (self->video_codec_hint);
  g_free (self->audio_language);

  G_OBJECT_CLASS (gst_tvproxy_demux_parent_class)->finalize (object);
}

static void
gst_tvproxy_demux_set_property (GObject *object, guint prop_id,
    const GValue *value, GParamSpec *pspec)
{
  GstTvproxyDemux *self = GST_TVPROXY_DEMUX (object);

  switch (prop_id) {
    case PROP_AUDIO_CHANNELS:
      self->audio_channels = g_value_get_int (value);
      break;
    case PROP_AUDIO_CODEC:
      g_free (self->audio_codec);
      self->audio_codec = g_value_dup_string (value);
      break;
    case PROP_VIDEO_CODEC_HINT:
      g_free (self->video_codec_hint);
      self->video_codec_hint = g_value_dup_string (value);
      break;
    case PROP_AUDIO_LANGUAGE:
      g_free (self->audio_language);
      self->audio_language = g_value_dup_string (value);
      break;
    default:
      G_OBJECT_WARN_INVALID_PROPERTY_ID (object, prop_id, pspec);
      break;
  }
}

static void
gst_tvproxy_demux_get_property (GObject *object, guint prop_id,
    GValue *value, GParamSpec *pspec)
{
  GstTvproxyDemux *self = GST_TVPROXY_DEMUX (object);

  switch (prop_id) {
    case PROP_AUDIO_CHANNELS:
      g_value_set_int (value, self->audio_channels);
      break;
    case PROP_AUDIO_CODEC:
      g_value_set_string (value, self->audio_codec);
      break;
    case PROP_VIDEO_CODEC_HINT:
      g_value_set_string (value, self->video_codec_hint);
      break;
    case PROP_AUDIO_LANGUAGE:
      g_value_set_string (value, self->audio_language);
      break;
    case PROP_VIDEO_INTERLACED:
      g_value_set_boolean (value, self->video_interlaced);
      break;
    default:
      G_OBJECT_WARN_INVALID_PROPERTY_ID (object, prop_id, pspec);
      break;
  }
}
