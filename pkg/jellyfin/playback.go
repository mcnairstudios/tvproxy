package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/hls"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

func (s *Server) playbackInfo(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	streamID := addDashes(itemID)

	var reqBody map[string]any
	json.NewDecoder(r.Body).Decode(&reqBody)

	stream, _ := s.streams.GetByID(r.Context(), streamID)

	videoCodec := "h264"
	audioCodec := "aac"
	var ticks int64

	if stream != nil {
		if stream.VODVCodec != "" {
			vc := strings.ToLower(stream.VODVCodec)
			if vc == "h264" || vc == "avc" {
				videoCodec = "h264"
			} else if vc == "hevc" || vc == "h265" {
				videoCodec = "hevc"
			}
		}
		if stream.VODDuration > 0 {
			ticks = secondsToTicks(stream.VODDuration)
		}
	}

	var mediaStreams []MediaStream
	if stream != nil {
		mediaStreams = s.buildMediaStreams(stream)
	} else {
		mediaStreams = []MediaStream{
			{Type: "Video", Codec: videoCodec, Index: 0, IsDefault: true, Width: 1920, Height: 1080},
			{Type: "Audio", Codec: audioCodec, Index: 1, IsDefault: true, Channels: 2, SampleRate: 0},
		}
	}

	playSessionID := itemID[:min(16, len(itemID))]
	ms := MediaSource{
		Protocol: "Http", ID: itemID, Type: "Default", Name: "Default",
		Container: "mp4", IsRemote: true,
		SupportsTranscoding:     true,
		SupportsDirectStream:    false,
		SupportsDirectPlay:      false,
		RunTimeTicks:            ticks,
		DefaultAudioStreamIndex: 1,
		TranscodingURL:          fmt.Sprintf("/Videos/%s/master.m3u8?MediaSourceId=%s&PlaySessionId=%s", itemID, itemID, playSessionID),
		TranscodingSubProtocol:  "hls",
		TranscodingContainer:    "ts",
		MediaStreams:             mediaStreams,
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"MediaSources":  []MediaSource{ms},
		"PlaySessionId": playSessionID,
	})
}

func (s *Server) hlsMasterPlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	streamID := addDashes(itemID)
	ctx := r.Context()

	var streamURL string
	var durationTicks int64
	var isLive bool

	if channel, err := s.channels.GetByID(ctx, streamID); err == nil && channel != nil {
		streamURL = fmt.Sprintf("%s/channel/%s?_port=8096", s.mainServerURLFromRequest(r), streamID)
		isLive = true
		_ = channel
	} else if stream, err := s.streams.GetByID(ctx, streamID); err == nil && stream != nil {
		streamURL = stream.URL
		if stream.VODDuration > 0 {
			durationTicks = secondsToTicks(stream.VODDuration)
		}
	}

	if streamURL == "" {
		http.Error(w, "stream not found", http.StatusNotFound)
		return
	}

	profile := hls.ProfileSettings{
		VideoCodec: "copy",
		AudioCodec: "aac",
	}
	sess := s.hlsManager.GetOrCreateSession(itemID, streamURL, 6, durationTicks, isLive, profile)
	playlistURL := fmt.Sprintf("/Videos/%s/main.m3u8", itemID)
	if isLive {
		playlistURL = fmt.Sprintf("/Videos/%s/live.m3u8", itemID)
	}
	hls.ServeMasterPlaylist(w, sess, playlistURL)
}

func (s *Server) hlsMediaPlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	sess := s.hlsManager.GetSession(itemID)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if !sess.IsLive && sess.IsDone() && sess.CurrentTranscodeIndex() == -1 {
		if err := sess.StartTranscode(context.Background(), 0, 0); err != nil {
			s.log.Error().Err(err).Str("session", itemID).Msg("failed to start initial transcode")
		}
	}

	hls.ServeMediaPlaylist(w, sess, fmt.Sprintf("hls1/main/"))
}

func (s *Server) hlsSegment(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	segmentFile := chi.URLParam(r, "segment")

	sess := s.hlsManager.GetSession(itemID)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if segmentFile == "init.mp4" {
		if err := s.hlsManager.RequestSegment(context.Background(), sess, 0, 0); err != nil {
			http.Error(w, "init segment not available", http.StatusNotFound)
			return
		}
		hls.ServeSegment(w, r, sess.InitSegmentPath())
		return
	}

	var segmentIndex int
	fmt.Sscanf(strings.TrimSuffix(segmentFile, ".mp4"), "seg%d", &segmentIndex)

	var runtimeTicks int64
	if rt := r.URL.Query().Get("runtimeTicks"); rt != "" {
		fmt.Sscanf(rt, "%d", &runtimeTicks)
	}

	if err := s.hlsManager.RequestSegment(context.Background(), sess, segmentIndex, runtimeTicks); err != nil {
		s.log.Error().Err(err).Int("segment", segmentIndex).Str("session", itemID).Msg("segment not available")
		http.Error(w, "segment not available", http.StatusNotFound)
		return
	}

	hls.ServeSegment(w, r, sess.SegmentPath(segmentIndex))
}

func (s *Server) hlsLivePlaylist(w http.ResponseWriter, r *http.Request) {
	s.hlsMediaPlaylist(w, r)
}

func (s *Server) videoStream(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	streamID := addDashes(itemID)
	ctx := r.Context()

	if channel, err := s.channels.GetByID(ctx, streamID); err == nil && channel != nil {
		username := s.resolveUsername(r)
		channelURL := fmt.Sprintf("%s/channel/%s?_port=8096&_user=%s", s.mainServerURLFromRequest(r), streamID, url.QueryEscape(username))
		http.Redirect(w, r, channelURL, http.StatusTemporaryRedirect)
		return
	}

	stream, err := s.streams.GetByID(ctx, streamID)
	if err != nil || stream == nil || stream.URL == "" {
		http.Error(w, "stream not found", http.StatusNotFound)
		return
	}

	streamURL := stream.URL
	if stream.UseWireGuard && s.WGProxyFunc != nil && strings.HasPrefix(streamURL, "http") {
		streamURL = s.WGProxyFunc(streamURL)
		s.log.Info().Str("stream", streamID).Str("proxy_url", streamURL).Msg("routing jellyfin video stream through WG proxy")
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Connection", "keep-alive")

	isLive := !strings.HasPrefix(streamURL, "/") && !strings.HasPrefix(streamURL, "file://")

	var seekMs int64
	seekTicks := r.URL.Query().Get("StartTimeTicks")
	if seekTicks == "" {
		seekTicks = r.URL.Query().Get("startTimeTicks")
	}
	if seekTicks != "" {
		var t int64
		fmt.Sscanf(seekTicks, "%d", &t)
		if t > 0 {
			seekMs = t / 10000
		}
	}

	s.log.Info().Str("stream", streamID).Str("url", streamURL).Bool("live", isLive).Msg("starting jellyfin video stream (avpipeline)")

	if err := service.RunAVPipeline(ctx, service.AVPipelineOpts{
		URL:          streamURL,
		Writer:       w,
		Format:       "mp4",
		VideoCodec:   "copy",
		AudioCodec:   "aac",
		IsLive:       isLive,
		SeekOffsetMs: seekMs,
		TimeoutSec:   10,
		Log:          s.log.With().Str("stream", streamID).Logger(),
	}); err != nil {
		s.log.Debug().Err(err).Str("stream", streamID).Msg("jellyfin video stream ended")
	}
}

func (s *Server) listSpecialFeatures(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, []BaseItemDto{})
}
