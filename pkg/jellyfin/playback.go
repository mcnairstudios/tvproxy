package jellyfin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) playbackInfo(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	streamID := addDashes(itemID)

	var reqBody map[string]any
	json.NewDecoder(r.Body).Decode(&reqBody)
	if dp, ok := reqBody["DeviceProfile"].(map[string]any); ok {
		if profiles, ok := dp["DirectPlayProfiles"].([]any); ok {
			s.log.Debug().Int("directPlayProfiles", len(profiles)).Msg("client device profile")
		}
	}

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
			{Type: "Audio", Codec: audioCodec, Index: 1, IsDefault: true, Channels: 2, SampleRate: 48000},
		}
	}

	ms := MediaSource{
		Protocol: "Http", ID: itemID, Type: "Default", Name: "Default",
		Container: "mp4", IsRemote: true,
		SupportsTranscoding:     true,
		SupportsDirectStream:    true,
		SupportsDirectPlay:      false,
		RunTimeTicks:            ticks,
		DefaultAudioStreamIndex: 1,
		TranscodingURL:          channelStreamURL(itemID),
		TranscodingSubProtocol:  "http",
		TranscodingContainer:    "mp4",
		MediaStreams:             mediaStreams,
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"MediaSources":  []MediaSource{ms},
		"PlaySessionId": itemID[:min(16, len(itemID))],
	})
}

func (s *Server) videoStream(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	streamID := addDashes(itemID)
	ctx := r.Context()

	if channel, err := s.channels.GetByID(ctx, streamID); err == nil && channel != nil {
		username := s.resolveUsername(r)
		channelURL := fmt.Sprintf("%s/channel/%s?_port=8096&_user=%s", s.mainServerURL(), streamID, url.QueryEscape(username))
		http.Redirect(w, r, channelURL, http.StatusTemporaryRedirect)
		return
	}

	stream, err := s.streams.GetByID(ctx, streamID)
	if err != nil || stream == nil || stream.URL == "" {
		http.Error(w, "stream not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Accept-Ranges", "none")
	w.Header().Set("Connection", "keep-alive")

	args := []string{
		"-analyzeduration", "2000000",
		"-probesize", "2000000",
		"-i", stream.URL,
		"-c:v", "copy",
		"-c:a", "aac",
		"-b:a", "192k",
		"-ac", "2",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4",
		"pipe:1",
	}

	s.log.Info().Str("stream", streamID).Str("url", stream.URL).Msg("starting jellyfin video stream")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = w

	if err := cmd.Run(); err != nil {
		s.log.Debug().Err(err).Str("stream", streamID).Msg("jellyfin video stream ended")
	}
}

func (s *Server) listSpecialFeatures(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, []BaseItemDto{})
}
