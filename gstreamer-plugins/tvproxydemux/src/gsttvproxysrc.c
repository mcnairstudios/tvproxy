#include "gsttvproxysrc.h"
#include <string.h>

GST_DEBUG_CATEGORY_STATIC (tvproxy_src_debug);
#define GST_CAT_DEFAULT tvproxy_src_debug

G_DEFINE_TYPE (GstTvproxySrc, gst_tvproxy_src, GST_TYPE_BIN)

enum {
  PROP_0,
  PROP_LOCATION,
  PROP_IS_LIVE,
  PROP_RTSP_TRANSPORT,
};

static GstStaticPadTemplate src_template = GST_STATIC_PAD_TEMPLATE ("src",
    GST_PAD_SRC,
    GST_PAD_ALWAYS,
    GST_STATIC_CAPS_ANY
    );

/* Forward declarations */
static GstStateChangeReturn gst_tvproxy_src_change_state (GstElement *element,
    GstStateChange transition);

/* RTSP pad-added: link rtspsrc dynamic pad to rtpmp2tdepay */
static void
rtspsrc_pad_added (GstElement *src, GstPad *pad, gpointer user_data)
{
  GstTvproxySrc *self = GST_TVPROXY_SRC (user_data);

  if (!self->depay)
    return;

  GstPad *sink = gst_element_get_static_pad (self->depay, "sink");
  if (!gst_pad_is_linked (sink)) {
    GstPadLinkReturn ret = gst_pad_link (pad, sink);
    GST_INFO_OBJECT (self, "RTSP pad-added link: %d", ret);
  }
  gst_object_unref (sink);
}

/* Build internal elements based on location URI scheme.
 * Called on READY→PAUSED transition so properties have been set. */
static gboolean
configure_elements (GstTvproxySrc *self)
{
  if (self->elements_configured)
    return TRUE;

  if (!self->location || self->location[0] == '\0') {
    GST_ERROR_OBJECT (self, "No location set");
    return FALSE;
  }

  /* Remove any existing internal elements */
  if (self->source) {
    gst_element_set_state (self->source, GST_STATE_NULL);
    gst_bin_remove (GST_BIN (self), self->source);
    self->source = NULL;
  }
  if (self->depay) {
    gst_element_set_state (self->depay, GST_STATE_NULL);
    gst_bin_remove (GST_BIN (self), self->depay);
    self->depay = NULL;
  }

  GstPad *ghost_target = NULL;

  if (g_str_has_prefix (self->location, "rtsp://") ||
      g_str_has_prefix (self->location, "rtsps://")) {
    /* RTSP mode: rtspsrc ! rtpmp2tdepay */
    GST_INFO_OBJECT (self, "RTSP mode: %s", self->location);

    self->source = gst_element_factory_make ("rtspsrc", "rtspsrc");
    if (!self->source) {
      GST_ERROR_OBJECT (self, "Failed to create rtspsrc");
      return FALSE;
    }

    guint protocols = 4; /* TCP */
    if (self->rtsp_transport && g_str_equal (self->rtsp_transport, "udp"))
      protocols = 1; /* UDP */

    g_object_set (self->source,
        "location", self->location,
        "latency", (guint) 0,
        "protocols", protocols,
        NULL);

    self->depay = gst_element_factory_make ("rtpmp2tdepay", "depay");
    if (!self->depay) {
      GST_ERROR_OBJECT (self, "Failed to create rtpmp2tdepay");
      return FALSE;
    }

    gst_bin_add_many (GST_BIN (self), self->source, self->depay, NULL);
    g_signal_connect (self->source, "pad-added",
        G_CALLBACK (rtspsrc_pad_added), self);

    ghost_target = gst_element_get_static_pad (self->depay, "src");

  } else if (g_str_has_prefix (self->location, "http://") ||
             g_str_has_prefix (self->location, "https://")) {
    /* HTTP mode: souphttpsrc */
    GST_INFO_OBJECT (self, "HTTP mode: %s", self->location);

    self->source = gst_element_factory_make ("souphttpsrc", "httpsrc");
    if (!self->source) {
      GST_ERROR_OBJECT (self, "Failed to create souphttpsrc");
      return FALSE;
    }

    g_object_set (self->source,
        "location", self->location,
        "do-timestamp", TRUE,
        "is-live", self->is_live,
        NULL);

    gst_bin_add (GST_BIN (self), self->source);
    ghost_target = gst_element_get_static_pad (self->source, "src");

  } else {
    /* File mode: filesrc */
    GST_INFO_OBJECT (self, "File mode: %s", self->location);

    self->source = gst_element_factory_make ("filesrc", "filesrc");
    if (!self->source) {
      GST_ERROR_OBJECT (self, "Failed to create filesrc");
      return FALSE;
    }

    g_object_set (self->source, "location", self->location, NULL);

    gst_bin_add (GST_BIN (self), self->source);
    ghost_target = gst_element_get_static_pad (self->source, "src");
  }

  if (ghost_target) {
    gst_ghost_pad_set_target (GST_GHOST_PAD (self->src_pad), ghost_target);
    gst_object_unref (ghost_target);
  }

  self->elements_configured = TRUE;
  return TRUE;
}

static GstStateChangeReturn
gst_tvproxy_src_change_state (GstElement *element, GstStateChange transition)
{
  GstTvproxySrc *self = GST_TVPROXY_SRC (element);

  switch (transition) {
    case GST_STATE_CHANGE_NULL_TO_READY:
      if (!configure_elements (self))
        return GST_STATE_CHANGE_FAILURE;
      break;
    case GST_STATE_CHANGE_READY_TO_NULL:
      self->elements_configured = FALSE;
      break;
    default:
      break;
  }

  return GST_ELEMENT_CLASS (gst_tvproxy_src_parent_class)->change_state (element, transition);
}

static void
gst_tvproxy_src_set_property (GObject *object, guint prop_id,
    const GValue *value, GParamSpec *pspec)
{
  GstTvproxySrc *self = GST_TVPROXY_SRC (object);

  switch (prop_id) {
    case PROP_LOCATION:
      g_free (self->location);
      self->location = g_value_dup_string (value);
      break;
    case PROP_IS_LIVE:
      self->is_live = g_value_get_boolean (value);
      break;
    case PROP_RTSP_TRANSPORT:
      g_free (self->rtsp_transport);
      self->rtsp_transport = g_value_dup_string (value);
      break;
    default:
      G_OBJECT_WARN_INVALID_PROPERTY_ID (object, prop_id, pspec);
      break;
  }
}

static void
gst_tvproxy_src_get_property (GObject *object, guint prop_id,
    GValue *value, GParamSpec *pspec)
{
  GstTvproxySrc *self = GST_TVPROXY_SRC (object);

  switch (prop_id) {
    case PROP_LOCATION:
      g_value_set_string (value, self->location);
      break;
    case PROP_IS_LIVE:
      g_value_set_boolean (value, self->is_live);
      break;
    case PROP_RTSP_TRANSPORT:
      g_value_set_string (value, self->rtsp_transport);
      break;
    default:
      G_OBJECT_WARN_INVALID_PROPERTY_ID (object, prop_id, pspec);
      break;
  }
}

static void
gst_tvproxy_src_finalize (GObject *object)
{
  GstTvproxySrc *self = GST_TVPROXY_SRC (object);
  g_free (self->location);
  g_free (self->rtsp_transport);
  G_OBJECT_CLASS (gst_tvproxy_src_parent_class)->finalize (object);
}

static void
gst_tvproxy_src_class_init (GstTvproxySrcClass *klass)
{
  GObjectClass *gobject_class = G_OBJECT_CLASS (klass);
  GstElementClass *element_class = GST_ELEMENT_CLASS (klass);

  gobject_class->set_property = gst_tvproxy_src_set_property;
  gobject_class->get_property = gst_tvproxy_src_get_property;
  gobject_class->finalize = gst_tvproxy_src_finalize;

  element_class->change_state = gst_tvproxy_src_change_state;

  g_object_class_install_property (gobject_class, PROP_LOCATION,
      g_param_spec_string ("location", "Location",
          "URI of the MPEG-TS stream (http://, rtsp://, or file path)",
          NULL,
          G_PARAM_READWRITE | G_PARAM_STATIC_STRINGS));

  g_object_class_install_property (gobject_class, PROP_IS_LIVE,
      g_param_spec_boolean ("is-live", "Is Live",
          "Whether the source is a live stream",
          TRUE,
          G_PARAM_READWRITE | G_PARAM_STATIC_STRINGS));

  g_object_class_install_property (gobject_class, PROP_RTSP_TRANSPORT,
      g_param_spec_string ("rtsp-transport", "RTSP Transport",
          "RTSP transport protocol: 'tcp' or 'udp'",
          "tcp",
          G_PARAM_READWRITE | G_PARAM_STATIC_STRINGS));

  gst_element_class_add_static_pad_template (element_class, &src_template);

  gst_element_class_set_static_metadata (element_class,
      "TV Proxy Source",
      "Source/Network",
      "HTTP/RTSP/file source for live MPEG-TS streams with sensible defaults",
      "tvproxy");

  GST_DEBUG_CATEGORY_INIT (tvproxy_src_debug, "tvproxysrc", 0,
      "TV Proxy Source");
}

static void
gst_tvproxy_src_init (GstTvproxySrc *self)
{
  self->location = NULL;
  self->is_live = TRUE;
  self->rtsp_transport = g_strdup ("tcp");
  self->source = NULL;
  self->depay = NULL;
  self->elements_configured = FALSE;

  /* Create ghost src pad with no target — set in configure_elements */
  self->src_pad = gst_ghost_pad_new_no_target ("src", GST_PAD_SRC);
  gst_element_add_pad (GST_ELEMENT (self), self->src_pad);
}
