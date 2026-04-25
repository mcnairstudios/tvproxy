package jellyfin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

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

	if channel, err := s.channels.GetByID(ctx, streamID); err == nil && channel != nil {
		playlistURL := fmt.Sprintf("/Videos/%s/live.m3u8", itemID)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		fmt.Fprintln(w, "#EXTM3U")
		fmt.Fprintf(w, "#EXT-X-STREAM-INF:BANDWIDTH=10000000\n")
		fmt.Fprintln(w, playlistURL)
		return
	}

	if s.vodService == nil {
		http.Error(w, "vod service unavailable", http.StatusServiceUnavailable)
		return
	}

	stream, err := s.streams.GetByID(ctx, streamID)
	if err != nil || stream == nil {
		http.Error(w, "stream not found", http.StatusNotFound)
		return
	}

	sessionID, _, outputDir, err := s.vodService.StartWatchingStreamHLS(ctx, streamID, r.Header.Get("User-Agent"), r.RemoteAddr)
	if err != nil {
		s.log.Error().Err(err).Str("stream_id", streamID).Msg("failed to start HLS session")
		http.Error(w, "failed to start session", http.StatusInternalServerError)
		return
	}

	s.log.Info().Str("stream_id", sessionID).Str("output_dir", outputDir).Msg("HLS master playlist requested")

	playlistURL := fmt.Sprintf("/Videos/%s/main.m3u8", itemID)
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	fmt.Fprintln(w, "#EXTM3U")
	fmt.Fprintf(w, "#EXT-X-STREAM-INF:BANDWIDTH=10000000\n")
	fmt.Fprintln(w, playlistURL)
}

func (s *Server) hlsMediaPlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	streamID := addDashes(itemID)
	ctx := r.Context()

	var outputDir string

	if channel, err := s.channels.GetByID(ctx, streamID); err == nil && channel != nil {
		channelURL := fmt.Sprintf("%s/channel/%s?_port=8096", s.mainServerURLFromRequest(r), streamID)
		s.log.Info().Str("channel_id", streamID).Str("url", channelURL).Msg("HLS live channel playlist — redirecting to proxy")
		http.Redirect(w, r, channelURL, http.StatusTemporaryRedirect)
		return
	}

	if s.vodService != nil {
		sess := s.vodService.GetSession(streamID)
		if sess != nil {
			outputDir = filepath.Join(sess.OutputDir, "segments")
		}
	}

	if outputDir == "" {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	playlistPath := filepath.Join(outputDir, "playlist.m3u8")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(playlistPath); err == nil && len(data) > 0 {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-cache, no-store")
			w.Write(data)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	http.Error(w, "playlist not ready", http.StatusServiceUnavailable)
}

func (s *Server) hlsSegment(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	segmentFile := chi.URLParam(r, "segment")
	streamID := addDashes(itemID)

	var outputDir string

	if s.vodService != nil {
		sess := s.vodService.GetSession(streamID)
		if sess != nil {
			outputDir = filepath.Join(sess.OutputDir, "segments")
		}
	}

	if outputDir == "" {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var segmentIndex int
	fmt.Sscanf(strings.TrimSuffix(strings.TrimSuffix(segmentFile, ".ts"), ".mp4"), "seg%d", &segmentIndex)

	segPath := filepath.Join(outputDir, fmt.Sprintf("seg%d.ts", segmentIndex))
	nextPath := filepath.Join(outputDir, fmt.Sprintf("seg%d.ts", segmentIndex+1))

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(segPath); err == nil {
			if _, err := os.Stat(nextPath); err == nil {
				break
			}
			if s.vodService.IsDone(streamID) {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if _, err := os.Stat(segPath); err != nil {
		s.log.Error().Err(err).Int("segment", segmentIndex).Str("session", itemID).Msg("segment not available")
		http.Error(w, "segment not available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "video/MP2T")
	http.ServeFile(w, r, segPath)
}

func (s *Server) hlsLivePlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	streamID := addDashes(itemID)
	ctx := r.Context()

	if channel, err := s.channels.GetByID(ctx, streamID); err == nil && channel != nil {
		channelURL := fmt.Sprintf("%s/channel/%s?_port=8096", s.mainServerURLFromRequest(r), streamID)
		http.Redirect(w, r, channelURL, http.StatusTemporaryRedirect)
		return
	}

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
