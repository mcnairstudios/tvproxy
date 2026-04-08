package jellyfin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
			uid := s.firstUserID(r.Context())
			s.tokens.Store(token, uid)
			s.touchJellyfinSession(r, uid)
			next.ServeHTTP(w, r)
			return
		}
		auth := s.authHeader(r)
		if strings.Contains(auth, "DeviceId=") {
			uid := s.firstUserID(r.Context())
			if uid != "" {
				s.touchJellyfinSession(r, uid)
			}
			next.ServeHTTP(w, r)
			return
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
	var req AuthenticateByNameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	_, _, err := s.auth.Login(r.Context(), req.Username, req.Pw)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

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

	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)
	s.tokens.Store(token, userID)

	now := time.Now()
	s.respondJSON(w, http.StatusOK, AuthenticationResult{
		User: &UserDto{
			Name:                  userName,
			ServerID:              s.serverID,
			ServerName:            s.serverName,
			ID:                    userID,
			HasPassword:           true,
			HasConfiguredPassword: true,
			LastLoginDate:         &now,
			LastActivityDate:      &now,
			Configuration: UserConfig{
				PlayDefaultAudioTrack: true,
				SubtitleMode:          "Default",
			},
			Policy: defaultPolicy(isAdmin),
		},
		SessionInfo: &SessionInfo{
			ID:                 token[:16],
			UserID:             userID,
			UserName:           userName,
			Client:             s.extractAuthField(r, "Client"),
			LastActivityDate:   now,
			DeviceName:         s.extractAuthField(r, "Device"),
			DeviceID:           s.extractAuthField(r, "DeviceId"),
			ApplicationVersion: s.extractAuthField(r, "Version"),
			IsActive:           true,
			ServerID:           s.serverID,
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

func defaultPolicy(isAdmin bool) UserPolicy {
	return UserPolicy{
		IsAdministrator:                 isAdmin,
		IsDisabled:                      false,
		EnableUserPreferenceAccess:      true,
		EnableRemoteControlOfOtherUsers: isAdmin,
		EnableSharedDeviceControl:       true,
		EnableRemoteAccess:              true,
		EnableLiveTvManagement:          isAdmin,
		EnableLiveTvAccess:              true,
		EnableMediaPlayback:             true,
		EnableAudioPlaybackTranscoding:  true,
		EnableVideoPlaybackTranscoding:  true,
		EnablePlaybackRemuxing:          true,
		EnableContentDownloading:        true,
		EnableAllChannels:               true,
		EnableAllFolders:                true,
		EnableAllDevices:                true,
		EnablePublicSharing:             true,
		AuthenticationProviderId:        "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
		PasswordResetProviderId:         "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
	}
}
