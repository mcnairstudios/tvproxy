# Fixes Needed

Review completed 2026-04-11. All three plugins build and register as a single .so — good decision. Two plugins need functionality fixes.

## tvproxysrc — Missing RTSP and file support

Currently HTTP-only (`souphttpsrc`). The spec requires three modes:

### What's missing

1. **RTSP mode**: When `location` starts with `rtsp://` or `rtsps://`, must create `rtspsrc ! rtpmp2tdepay` internally. `rtspsrc` has dynamic pads — the `pad-added` signal must link `rtspsrc` to `rtpmp2tdepay` inside the bin. The ghost `src` pad targets `rtpmp2tdepay`'s src pad.

2. **File mode**: When `location` doesn't start with `http://` or `rtsp://`, must create `filesrc` internally.

3. **`is-live` property**: Default true. For HTTP sets `do-timestamp=true is-live=true` on souphttpsrc. For file mode, should be false.

4. **`rtsp-transport` property**: Default "tcp". Maps to `rtspsrc protocols` property (4=TCP, 1=UDP).

### RTSP implementation pattern

```c
static void
rtspsrc_pad_added (GstElement *src, GstPad *pad, gpointer user_data)
{
  GstTvproxySrc *self = GST_TVPROXY_SRC (user_data);
  GstPad *sink = gst_element_get_static_pad (self->depay, "sink");
  if (!gst_pad_is_linked (sink))
    gst_pad_link (pad, sink);
  gst_object_unref (sink);
}

// In init or state change:
if (g_str_has_prefix (self->location, "rtsp://")) {
  self->rtspsrc = gst_element_factory_make ("rtspsrc", "rtspsrc");
  g_object_set (self->rtspsrc, "location", self->location,
      "latency", (guint) 0, "protocols", (guint) 4, NULL);
  self->depay = gst_element_factory_make ("rtpmp2tdepay", "depay");
  gst_bin_add_many (GST_BIN (self), self->rtspsrc, self->depay, NULL);
  g_signal_connect (self->rtspsrc, "pad-added",
      G_CALLBACK (rtspsrc_pad_added), self);
  // Ghost pad targets depay src
  GstPad *target = gst_element_get_static_pad (self->depay, "src");
  gst_ghost_pad_set_target (GST_GHOST_PAD (self->src_pad), target);
  gst_object_unref (target);
}
```

### RTSP properties for SAT>IP

```
rtspsrc latency=0 protocols=4 (TCP)
```

SAT>IP URLs look like: `rtsp://192.168.1.149/?freq=545.833&msys=dvbt2&mtype=256qam&pids=0,6650,6601&bw=8&plp=0`

### Test

```bash
# HTTP (should already work):
gst-launch-1.0 -e tvproxysrc location=http://192.168.1.186:5004/auto/v101 ! fakesink

# RTSP (currently broken):
gst-launch-1.0 -e tvproxysrc location="rtsp://192.168.1.149/?freq=545.833&msys=dvbt2&mtype=256qam&pids=0,6650,6601,6602,6606,6605&bw=8&plp=0" ! fakesink

# File:
gst-launch-1.0 -e tvproxysrc location=/tmp/test.ts is-live=false ! fakesink
```

---

## tvproxymux — Missing auto-parser insertion

Currently ghost-pads directly to the internal mp4mux/mpegtsmux. The spec requires auto-inserting the correct parser with `config-interval=-1` based on input video caps.

### What's missing

1. **Video parser auto-detection**: When the `video` request pad links, detect input caps and insert:
   - `video/x-h264` → `h264parse config-interval=-1`
   - `video/x-h265` → `h265parse config-interval=-1`  
   - `video/mpeg` → `mpegvideoparse`

2. **Audio parser**: Insert `aacparse` before the mux for audio.

3. **config-interval=-1 is ESSENTIAL**: Without this, VPS/SPS/PPS parameter sets are only at stream start. VLC and browsers can't start decoding if they miss the start. This was the #1 cause of "video won't play" in our testing.

### Implementation approach

Instead of ghost-padding the request pad directly to the muxer, insert a parser element in between:

```c
static GstPad *
gst_tvproxy_mux_request_new_pad (GstElement *element, GstPadTemplate *templ,
    const gchar *name, const GstCaps *caps)
{
  GstTvproxyMux *self = GST_TVPROXY_MUX (element);
  
  if (g_str_equal (templ_name, "video")) {
    // Create parser — default to h264parse, will be replaced on caps change
    GstElement *parser = gst_element_factory_make ("h264parse", "vparse");
    g_object_set (parser, "config-interval", (gint) -1, NULL);
    gst_bin_add (GST_BIN (self), parser);
    
    // Link parser to muxer
    GstPad *mux_sink = gst_element_request_pad_simple (self->muxer, "video_%u");
    gst_element_link_pads (parser, "src", self->muxer, NULL);
    
    // Ghost pad exposes parser's sink
    GstPad *parser_sink = gst_element_get_static_pad (parser, "sink");
    self->video_pad = gst_ghost_pad_new ("video", parser_sink);
    gst_object_unref (parser_sink);
    
    // Sync state
    gst_element_sync_state_with_parent (parser);
    gst_pad_set_active (self->video_pad, TRUE);
    gst_element_add_pad (element, self->video_pad);
    return self->video_pad;
  }
  
  if (g_str_equal (templ_name, "audio")) {
    GstElement *parser = gst_element_factory_make ("aacparse", "aparse");
    gst_bin_add (GST_BIN (self), parser);
    gst_element_link (parser, self->muxer);
    
    GstPad *parser_sink = gst_element_get_static_pad (parser, "sink");
    self->audio_pad = gst_ghost_pad_new ("audio", parser_sink);
    gst_object_unref (parser_sink);
    
    gst_element_sync_state_with_parent (parser);
    gst_pad_set_active (self->audio_pad, TRUE);
    gst_element_add_pad (element, self->audio_pad);
    return self->audio_pad;
  }
}
```

For auto-detection of h264 vs h265 vs mpeg2, either:
- Use a caps probe on the ghost pad to detect on first buffer
- Or start with h264parse (most common) and swap if caps don't match

### Test

```bash
# This should "just work" — parser auto-inserted:
gst-launch-1.0 -e \
  tvproxysrc location=http://192.168.1.186:5004/auto/v101 \
  ! tvproxydemux name=d \
  d.video ! m.video \
  d.audio ! m.audio \
  tvproxymux name=m ! filesink location=/tmp/test.mp4

# Verify config-interval is working:
# Play in VLC — video should start immediately, not after waiting for keyframe
```

---

## tvproxydemux — Looks good

No issues found. Audio chain with aacparse works, video auto-detection works, static pads work. The 3s benchmark is meeting the spec.

---

## Build note

All three in one .so is the right call. The Dockerfile in tvproxy can be simplified to build just this one meson project instead of three separate builds. The plugin.c registers all three elements.
