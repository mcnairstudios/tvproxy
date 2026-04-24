package jellyfin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.extractToken(r)
		if token != "" {
			if userID, ok := s.tokens.Load(token); ok {
				s.touchJellyfinSession(r, userID.(string))
				next.ServeHTTP(w, r)
				return
			}
			if userID := s.firstUserID(r.Context()); userID != "" {
				s.tokens.Store(token, userID)
				s.saveState()
				s.log.Info().Str("path", r.URL.Path).Msg("auto-registered unknown token")
				s.touchJellyfinSession(r, userID)
				next.ServeHTTP(w, r)
				return
			}
		} else {
			s.log.Warn().Str("path", r.URL.Path).Msg("no token in request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"Code":"NotAuthenticated"}`))
	})
}

func (s *Server) touchJellyfinSession(r *http.Request, userID string) {
	if s.activityService == nil {
		return
	}
	username := s.resolveUsernameByID(r, userID)
	ua := r.UserAgent()
	if decoded, err := url.QueryUnescape(ua); err == nil {
		ua = decoded
	}
	s.activityService.TouchUser(userID, username, "Jellyfin", r.RemoteAddr, ua)
}

func (s *Server) resolveUsernameByID(r *http.Request, userID string) string {
	users, _ := s.auth.ListUsers(r.Context())
	for _, u := range users {
		if u.ID == userID {
			return u.Username
		}
	}
	return ""
}

func (s *Server) authenticateByName(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	s.log.Debug().Str("body", string(bodyBytes)).Msg("auth request body")
	var req AuthenticateByNameRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	pw := req.Pw
	if pw == "" {
		pw = req.Password
	}
	s.log.Info().Str("username", req.Username).Bool("has_pw", req.Pw != "").Bool("has_password", req.Password != "").Msg("auth attempt")
	_, _, err := s.auth.Login(r.Context(), req.Username, pw)
	if err != nil {
		s.log.Warn().Str("username", req.Username).Err(err).Msg("auth failed")
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	s.log.Info().Str("username", req.Username).Msg("auth success")

	users, _ := s.auth.ListUsers(r.Context())
	var userID, userName string
	var isAdmin bool
	for _, u := range users {
		if strings.EqualFold(u.Username, req.Username) {
			userID = u.ID
			userName = u.Username
			isAdmin = u.IsAdmin
			break
		}
	}

	jfUserID := jellyfinID(userID)
	tokenBytes := make([]byte, 16)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)
	s.tokens.Store(token, userID)
	s.saveState()

	nowJF := JellyfinTime{time.Now().UTC()}
	sessionIDBytes := make([]byte, 16)
	rand.Read(sessionIDBytes)
	sessionID := hex.EncodeToString(sessionIDBytes)
	s.respondJSON(w, http.StatusOK, AuthenticationResult{
		User: &UserDto{
			Name:                  userName,
			ServerID:              s.serverID,
			ID:                    jfUserID,
			HasPassword:           true,
			HasConfiguredPassword: true,
			LastLoginDate:         &nowJF,
			LastActivityDate:      &nowJF,
			Configuration:         defaultUserConfig(),
			Policy:                defaultPolicy(isAdmin),
		},
		SessionInfo: &SessionInfo{
			PlayState:       &PlayState{RepeatMode: "RepeatNone", PlaybackOrder: "Default"},
			AdditionalUsers: []any{},
			Capabilities: &SessionCapabilities{
				PlayableMediaTypes:           jellyfinPlayableMediaTypes(),
				SupportedCommands:            jellyfinSupportedCommands(),
				SupportsMediaControl:         true,
				SupportsPersistentIdentifier: false,
			},
			RemoteEndPoint:           r.RemoteAddr,
			PlayableMediaTypes:       jellyfinPlayableMediaTypes(),
			ID:                       sessionID,
			UserID:                   jfUserID,
			UserName:                 userName,
			Client:                   s.extractAuthField(r, "Client"),
			LastActivityDate:         nowJF,
			LastPlaybackCheckIn:      "0001-01-01T00:00:00.0000000Z",
			DeviceName:               s.extractAuthField(r, "Device"),
			DeviceID:                 s.extractAuthField(r, "DeviceId"),
			ApplicationVersion:       s.extractAuthField(r, "Version"),
			IsActive:                 true,
			NowPlayingQueue:          []any{},
			NowPlayingQueueFullItems: []any{},
			ServerID:                 s.serverID,
			SupportedCommands:        jellyfinSupportedCommands(),
		},
		AccessToken: token,
		ServerID:    s.serverID,
	})
}

func (s *Server) extractToken(r *http.Request) string {
	if t := r.URL.Query().Get("api_key"); t != "" {
		return t
	}
	if t := r.URL.Query().Get("ApiKey"); t != "" {
		return t
	}
	if t := r.Header.Get("X-MediaBrowser-Token"); t != "" {
		return t
	}
	if t := r.Header.Get("X-Emby-Token"); t != "" {
		return t
	}
	return s.extractAuthField(r, "Token")
}

func (s *Server) extractAuthField(r *http.Request, key string) string {
	auth := s.authHeader(r)
	if !strings.Contains(auth, key+"=") {
		return ""
	}
	parts := strings.Split(auth, key+"=")
	if len(parts) < 2 {
		return ""
	}
	val := parts[1]
	if strings.HasPrefix(val, "\"") {
		end := strings.Index(val[1:], "\"")
		if end >= 0 {
			return val[1 : end+1]
		}
	}
	end := strings.IndexAny(val, ", ")
	if end >= 0 {
		return val[:end]
	}
	return val
}

func (s *Server) authHeader(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		return auth
	}
	return r.Header.Get("X-Emby-Authorization")
}

func (s *Server) authenticatedUserID(r *http.Request) string {
	token := s.extractToken(r)
	if v, ok := s.tokens.Load(token); ok {
		return v.(string)
	}
	return ""
}

func (s *Server) resolveUsername(r *http.Request) string {
	userID := s.authenticatedUserID(r)
	if userID == "" {
		return ""
	}
	users, _ := s.auth.ListUsers(r.Context())
	for _, u := range users {
		if u.ID == userID {
			return u.Username
		}
	}
	return ""
}

func (s *Server) firstUserID(ctx context.Context) string {
	users, _ := s.auth.ListUsers(ctx)
	if len(users) > 0 {
		return users[0].ID
	}
	return ""
}

func defaultUserConfig() UserConfig {
	return UserConfig{
		PlayDefaultAudioTrack:      true,
		SubtitleMode:               "Default",
		GroupedFolders:             []string{},
		OrderedViews:               []string{},
		LatestItemsExcludes:        []string{},
		MyMediaExcludes:            []string{},
		HidePlayedInLatest:         true,
		RememberAudioSelections:    true,
		RememberSubtitleSelections: true,
		EnableNextEpisodeAutoPlay:  true,
		CastReceiverId:             "F007D354",
	}
}

func jellyfinPlayableMediaTypes() []string {
	return []string{"Audio", "Video"}
}

func jellyfinSupportedCommands() []string {
	return []string{
		"MoveUp", "MoveDown", "MoveLeft", "MoveRight",
		"PageUp", "PageDown", "PreviousLetter", "NextLetter",
		"ToggleOsd", "ToggleContextMenu", "Select", "Back",
		"SendKey", "SendString", "GoHome", "GoToSettings",
		"VolumeUp", "VolumeDown", "Mute", "Unmute", "ToggleMute",
		"SetVolume", "SetAudioStreamIndex", "SetSubtitleStreamIndex",
		"DisplayContent", "GoToSearch", "DisplayMessage",
		"SetRepeatMode", "SetShuffleQueue",
		"ChannelUp", "ChannelDown",
		"PlayMediaSource", "PlayTrailers",
	}
}

func defaultPolicy(isAdmin bool) UserPolicy {
	return UserPolicy{
		IsAdministrator:                  isAdmin,
		IsHidden:                         isAdmin,
		IsDisabled:                       false,
		BlockedTags:                      []string{},
		AllowedTags:                      []string{},
		EnableUserPreferenceAccess:       true,
		AccessSchedules:                  []any{},
		BlockUnratedItems:                []string{},
		EnableRemoteControlOfOtherUsers:  isAdmin,
		EnableSharedDeviceControl:        true,
		EnableRemoteAccess:               true,
		EnableLiveTvManagement:           isAdmin,
		EnableLiveTvAccess:               true,
		EnableMediaPlayback:              true,
		EnableAudioPlaybackTranscoding:   true,
		EnableVideoPlaybackTranscoding:   true,
		EnablePlaybackRemuxing:           true,
		EnableContentDeletion:            isAdmin,
		EnableContentDeletionFromFolders: []string{},
		EnableContentDownloading:         true,
		EnableSyncTranscoding:            true,
		EnableMediaConversion:            true,
		EnabledDevices:                   []string{},
		EnableAllDevices:                 true,
		EnabledChannels:                  []string{},
		EnableAllChannels:                true,
		EnabledFolders:                   []string{},
		EnableAllFolders:                 true,
		LoginAttemptsBeforeLockout:       -1,
		EnablePublicSharing:              true,
		BlockedMediaFolders:              []string{},
		BlockedChannels:                  []string{},
		AuthenticationProviderId:         "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
		PasswordResetProviderId:          "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
		SyncPlayAccess:                   "CreateAndJoinGroups",
	}
}
