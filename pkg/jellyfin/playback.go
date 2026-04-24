package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/hls"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

func (s *Server) playbackInfo(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	streamID := addDashes(stripDashes(itemID))

	bodyBytes, _ := io.ReadAll(r.Body)
	s.log.Debug().Str("body", string(bodyBytes)).Msg("PlaybackInfo request")
	var reqBody map[string]any
	json.Unmarshal(bodyBytes, &reqBody)

	stream, _ := s.streams.GetByID(r.Context(), streamID)
	if stream == nil {
		s.respondJSON(w, http.StatusOK, map[string]any{
			"MediaSources":  []MediaSource{},
			"PlaySessionId": "",
		})
		return
	}

	profile := s.jellyfinProfile(r.Context())
	container := "mp4"
	if profile != nil && profile.Container != "" {
		container = profile.Container
	}

	var ticks int64
	if stream.VODDuration > 0 {
		ticks = secondsToTicks(stream.VODDuration)
	}
	if ticks == 0 && s.tmdbClient != nil {
		if m := s.tmdbClient.LookupMovie(stream.Name); m != nil && m.TMDBID > 0 {
			if details, err := s.tmdbClient.Details("movie", fmt.Sprintf("%d", m.TMDBID)); err == nil {
				if dm, ok := details.(map[string]any); ok {
					if rt, ok := dm["runtime"].(float64); ok && rt > 0 {
						ticks = int64(rt) * 60 * 10000000
					}
				}
			}
		}
	}

	videoCodec := "hevc"
	audioCodec := "aac"
	if profile != nil {
		if profile.VideoCodec != "" && profile.VideoCodec != "copy" && profile.VideoCodec != "default" {
			videoCodec = profile.VideoCodec
		}
		if profile.AudioCodec != "" && profile.AudioCodec != "copy" && profile.AudioCodec != "default" {
			audioCodec = profile.AudioCodec
		}
	}

	mediaStreams := s.buildMediaStreams(stream)
	for i := range mediaStreams {
		if mediaStreams[i].Type == "Video" {
			mediaStreams[i].Codec = videoCodec
		} else if mediaStreams[i].Type == "Audio" && audioCodec != "copy" {
			mediaStreams[i].Codec = audioCodec
		}
	}

	cleanID := stripDashes(itemID)
	playSessionID := cleanID[:min(16, len(cleanID))]

	token := s.extractToken(r)
	deviceID := s.extractAuthField(r, "DeviceId")
	transcodingURL := fmt.Sprintf("/videos/%s/master.m3u8?DeviceId=%s&MediaSourceId=%s&VideoCodec=%s&AudioCodec=%s&AudioStreamIndex=1&SegmentContainer=ts&PlaySessionId=%s&ApiKey=%s&TranscodeReasons=DirectPlayError",
		cleanID, url.QueryEscape(deviceID), cleanID, videoCodec, audioCodec, playSessionID, token)

	ms := MediaSource{
		Protocol: "Http", ID: cleanID, Type: "Default", Name: stream.Name,
		Container: container, IsRemote: true,
		SupportsTranscoding:     true,
		SupportsDirectStream:    false,
		SupportsDirectPlay:      false,
		IsInfiniteStream:        false,
		RequiresOpening:         false,
		RequiresClosing:         false,
		RunTimeTicks:            ticks,
		DefaultAudioStreamIndex: 1,
		TranscodingURL:          transcodingURL,
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

	profile := s.jellyfinProfile(ctx)
	videoCodec := "copy"
	audioCodec := "aac"
	format := "mp4"
	if profile != nil {
		if profile.VideoCodec != "" {
			videoCodec = profile.VideoCodec
		}
		if profile.AudioCodec != "" {
			audioCodec = profile.AudioCodec
		}
		if profile.Container != "" {
			format = profile.Container
		}
	}

	s.log.Info().Str("stream", streamID).Str("url", streamURL).Str("video", videoCodec).Str("audio", audioCodec).Bool("live", isLive).Msg("starting jellyfin video stream (avpipeline)")

	if err := service.RunAVPipeline(ctx, service.AVPipelineOpts{
		URL:          streamURL,
		Writer:       w,
		Format:       format,
		VideoCodec:   videoCodec,
		AudioCodec:   audioCodec,
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
