package jellyfin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/service"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tmdb"
)

type Server struct {
	serverID     string
	serverName   string
	baseURL      string
	auth         *service.AuthService
	channels     store.ChannelStore
	streams      store.StreamReader
	epg          store.EPGStore
	logoService  *service.LogoService
	tmdbClient   *tmdb.Client
	log          zerolog.Logger
	tokens       sync.Map
}

func NewServer(serverName, baseURL string, auth *service.AuthService, channels store.ChannelStore, streams store.StreamReader, epg store.EPGStore, logoService *service.LogoService, tmdbClient *tmdb.Client, log zerolog.Logger) *Server {
	id := make([]byte, 16)
	rand.Read(id)
	h := hex.EncodeToString(id)
	guid := h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	return &Server{
		serverID:    guid,
		serverName:  serverName,
		baseURL:     baseURL,
		auth:        auth,
		channels:    channels,
		streams:     streams,
		epg:         epg,
		logoService: logoService,
		tmdbClient:  tmdbClient,
		log:         log.With().Str("component", "jellyfin").Logger(),
	}
}

func (s *Server) Router() chi.Router {
	r := chi.NewRouter()

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("Jellyfin Server"))
	})
	r.Get("/web", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/web/index.html", http.StatusFound)
	})
	r.Get("/web/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/web/index.html", http.StatusFound)
	})
	r.Get("/web/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<!DOCTYPE html><html><body><h1>TVProxy Jellyfin API</h1><p>Use a Jellyfin client app to connect.</p></body></html>"))
	})
	r.Get("/web/{file}", func(w http.ResponseWriter, r *http.Request) {
		file := chi.URLParam(r, "file")
		if file == "config.json" {
			s.respondJSON(w, http.StatusOK, map[string]any{
				"menuLinks":    []any{},
				"multiserver":  false,
				"themes":       []any{},
				"plugins":      []any{},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	r.Get("/System/Info/Public", s.systemInfoPublic)
	r.Get("/System/Info", s.systemInfo)
	r.Get("/System/Ping", s.ping)
	r.Post("/System/Ping", s.ping)

	r.Get("/Branding/Configuration", s.brandingConfig)
	r.Get("/Branding/Css", s.brandingCSS)

	r.Get("/QuickConnect/Enabled", s.quickConnectEnabled)

	r.Get("/Users/Public", s.usersPublic)
	r.Post("/Users/AuthenticateByName", s.authenticateByName)

	r.Get("/Branding/Splashscreen", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	r.Get("/UserImage", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	r.Head("/UserImage", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	r.Get("/Items/{itemId}/Images/{imageType}", s.getImage)
	r.Get("/Items/{itemId}/Images/{imageType}/{imageIndex}", s.getImage)
	r.Head("/Items/{itemId}/Images/{imageType}", s.getImage)
	r.Head("/Items/{itemId}/Images/{imageType}/{imageIndex}", s.getImage)
	r.Get("/Videos/{itemId}/stream", s.videoStream)
	r.Get("/Videos/{itemId}/stream.{container}", s.videoStream)
	r.Head("/Videos/{itemId}/stream", s.videoStream)
	r.Head("/Videos/{itemId}/stream.{container}", s.videoStream)

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)

		r.Get("/Users/Me", s.usersMe)
		r.Get("/Users", s.usersList)
		r.Get("/Users/{userId}", s.userByID)

		r.Get("/UserViews", s.userViews)

		r.Get("/Items", s.getItems)
		r.Get("/Items/Filters", s.getFilters)
		r.Get("/Items/{itemId}", s.getItem)
		r.Get("/Items/Latest", s.getLatest)
		r.Get("/Items/Resume", s.getResume)
		r.Get("/UserItems/Resume", s.getResume)
		r.Get("/Users/{userId}/Items", s.getItems)
		r.Get("/Users/{userId}/Items/Latest", s.getLatest)
		r.Get("/Users/{userId}/Items/Resume", s.getResume)
		r.Get("/Users/{userId}/Items/{itemId}", s.getItem)
		r.Get("/Shows/NextUp", s.getResume)

		r.Get("/Shows/{seriesId}/Seasons", s.getSeasons)
		r.Get("/Shows/{seriesId}/Episodes", s.getEpisodes)

		r.Get("/Items/{itemId}/Similar", s.getSimilar)
		r.Get("/Items/{itemId}/LocalTrailers", s.getSpecialFeatures)
		r.Get("/Items/{itemId}/SpecialFeatures", s.getSpecialFeatures)
		r.Get("/Items/{itemId}/ThemeMedia", s.getSpecialFeatures)
		r.Get("/Items/{itemId}/ThemeSongs", s.getSpecialFeatures)
		r.Get("/Items/{itemId}/ThemeVideos", s.getSpecialFeatures)
		r.Get("/Items/{itemId}/InstantMix", s.getSpecialFeatures)

		r.Post("/Items/{itemId}/PlaybackInfo", s.playbackInfo)
		r.Get("/Items/Counts", s.itemCounts)
		r.Get("/Items/Suggestions", s.getLatest)

		r.Get("/Persons", s.emptyQueryResult)
		r.Get("/Persons/{personId}", func(w http.ResponseWriter, r *http.Request) {
			s.respondJSON(w, http.StatusOK, BaseItemDto{
				Name: chi.URLParam(r, "personId"), ServerID: s.serverID,
				ID: chi.URLParam(r, "personId"), Type: "Person",
			})
		})
		r.Get("/Studios", s.emptyQueryResult)
		r.Get("/Artists", s.emptyQueryResult)
		r.Get("/Genres", s.emptyQueryResult)

		r.Get("/Sessions", s.sessionsGet)
		r.Get("/LiveTv/Info", s.liveTvInfo)
		r.Get("/LiveTv/Channels", s.liveTvChannels)
		r.Get("/LiveTv/Programs", s.liveTvPrograms)
		r.Post("/LiveTv/Programs", s.liveTvPrograms)
		r.Get("/LiveTv/GuideInfo", s.liveTvGuideInfo)

		r.Get("/Playback/BitrateTest", s.bitrateTest)

		r.Post("/Sessions/Capabilities/Full", s.sessionsCapabilities)
		r.Post("/Sessions/Playing", s.sessionsPlaying)
		r.Post("/Sessions/Playing/Progress", s.sessionsPlaying)
		r.Post("/Sessions/Playing/Stopped", s.sessionsPlaying)

		r.Get("/DisplayPreferences/{id}", s.displayPreferences)
		r.Post("/DisplayPreferences/{id}", s.sessionsCapabilities)

		r.Post("/UserPlayedItems/{itemId}", s.markPlayed)
		r.Delete("/UserPlayedItems/{itemId}", s.markPlayed)
		r.Post("/UserFavoriteItems/{itemId}", s.markFavorite)
		r.Delete("/UserFavoriteItems/{itemId}", s.markFavorite)
	})

	return r
}

func (s *Server) respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8; profile=\"CamelCase\"")
	w.Header().Set("X-Application", "Jellyfin")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) systemInfoPublic(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, PublicSystemInfo{
		LocalAddress:           s.baseURL,
		ServerName:             s.serverName,
		Version:                "10.10.6",
		ProductName:            "Jellyfin Server",
		OperatingSystem:        "Linux",
		ID:                     s.serverID,
		StartupWizardCompleted: true,
	})
}

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]any{
		"LocalAddress":             s.baseURL,
		"ServerName":               s.serverName,
		"Version":                  "10.10.6",
		"ProductName":              "Jellyfin Server",
		"OperatingSystem":          "Linux",
		"OperatingSystemDisplayName": "Linux",
		"Id":                       s.serverID,
		"StartupWizardCompleted":   true,
		"HasPendingRestart":        false,
		"IsShuttingDown":           false,
		"SupportsLibraryMonitor":   false,
		"WebSocketPortNumber":      0,
		"CanSelfRestart":           false,
		"CanLaunchWebBrowser":      false,
		"HasUpdateAvailable":       false,
	})
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("Jellyfin Server"))
}

func (s *Server) brandingConfig(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BrandingConfiguration{
		LoginDisclaimer:     "",
		CustomCSS:           "",
		SplashscreenEnabled: false,
	})
}

func (s *Server) brandingCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	w.Write([]byte(""))
}

func (s *Server) quickConnectEnabled(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, false)
}

func (s *Server) usersPublic(w http.ResponseWriter, r *http.Request) {
	users, _ := s.auth.ListUsers(r.Context())
	var result []UserDto
	for _, u := range users {
		result = append(result, UserDto{
			Name:                  u.Username,
			ServerID:              s.serverID,
			ID:                    u.ID,
			HasPassword:           true,
			HasConfiguredPassword: true,
			Policy:                s.defaultPolicy(u.IsAdmin),
			Configuration: UserConfig{
				PlayDefaultAudioTrack: true,
				SubtitleMode:          "Default",
			},
		})
	}
	if result == nil {
		result = []UserDto{}
	}
	s.respondJSON(w, http.StatusOK, result)
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
			Policy: s.defaultPolicy(isAdmin),
		},
		SessionInfo: &SessionInfo{
			ID:                 token[:16],
			UserID:             userID,
			UserName:           userName,
			Client:             s.extractClient(r),
			LastActivityDate:   now,
			DeviceName:         s.extractDevice(r),
			DeviceID:           s.extractDeviceID(r),
			ApplicationVersion: s.extractVersion(r),
			IsActive:           true,
			ServerID:           s.serverID,
		},
		AccessToken: token,
		ServerID:    s.serverID,
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.extractToken(r)
		if token != "" {
			if _, ok := s.tokens.Load(token); ok {
				next.ServeHTTP(w, r)
				return
			}
			s.tokens.Store(token, s.getFirstUserID(r.Context()))
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth == "" {
			auth = r.Header.Get("X-Emby-Authorization")
		}
		if strings.Contains(auth, "DeviceId=") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"Code":"NotAuthenticated"}`))
	})
}

func (s *Server) getFirstUserID(ctx context.Context) string {
	users, _ := s.auth.ListUsers(ctx)
	if len(users) > 0 {
		return users[0].ID
	}
	return ""
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
	auth := r.Header.Get("Authorization")
	if auth == "" {
		auth = r.Header.Get("X-Emby-Authorization")
	}
	if strings.Contains(auth, "Token=") {
		parts := strings.Split(auth, "Token=")
		if len(parts) > 1 {
			token := parts[1]
			if strings.HasPrefix(token, "\"") {
				end := strings.Index(token[1:], "\"")
				if end >= 0 {
					return token[1 : end+1]
				}
			}
			end := strings.IndexAny(token, ", ")
			if end >= 0 {
				return token[:end]
			}
			return strings.TrimSpace(token)
		}
	}
	return ""
}

func (s *Server) extractFromAuth(r *http.Request, key string) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		auth = r.Header.Get("X-Emby-Authorization")
	}
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

func (s *Server) extractClient(r *http.Request) string {
	return s.extractFromAuth(r, "Client")
}

func (s *Server) extractDevice(r *http.Request) string {
	return s.extractFromAuth(r, "Device")
}

func (s *Server) extractDeviceID(r *http.Request) string {
	return s.extractFromAuth(r, "DeviceId")
}

func (s *Server) extractVersion(r *http.Request) string {
	return s.extractFromAuth(r, "Version")
}

func (s *Server) getUserID(r *http.Request) string {
	token := s.extractToken(r)
	if v, ok := s.tokens.Load(token); ok {
		return v.(string)
	}
	return ""
}

func (s *Server) defaultPolicy(isAdmin bool) UserPolicy {
	return UserPolicy{
		IsAdministrator:                isAdmin,
		IsDisabled:                     false,
		EnableUserPreferenceAccess:     true,
		EnableRemoteControlOfOtherUsers: isAdmin,
		EnableSharedDeviceControl:      true,
		EnableRemoteAccess:             true,
		EnableLiveTvManagement:         isAdmin,
		EnableLiveTvAccess:             true,
		EnableMediaPlayback:            true,
		EnableAudioPlaybackTranscoding: true,
		EnableVideoPlaybackTranscoding: true,
		EnablePlaybackRemuxing:         true,
		EnableContentDownloading:       true,
		EnableAllChannels:              true,
		EnableAllFolders:               true,
		EnableAllDevices:               true,
		EnablePublicSharing:            true,
		AuthenticationProviderId:       "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
		PasswordResetProviderId:        "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
	}
}

func (s *Server) usersMe(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	now := time.Now()
	s.respondJSON(w, http.StatusOK, UserDto{
		Name:                  "admin",
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
		Policy: s.defaultPolicy(true),
	})
}

func (s *Server) usersList(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	now := time.Now()
	s.respondJSON(w, http.StatusOK, []UserDto{
		{
			Name:                  "admin",
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
			Policy: s.defaultPolicy(true),
		},
	})
}

func (s *Server) userByID(w http.ResponseWriter, r *http.Request) {
	s.usersMe(w, r)
}

func (s *Server) sessionsCapabilities(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sessionsPlaying(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) bitrateTest(w http.ResponseWriter, r *http.Request) {
	size := 1000000
	if s := r.URL.Query().Get("size"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 10000000 {
			size = n
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(size))
	buf := make([]byte, 65536)
	written := 0
	for written < size {
		chunk := size - written
		if chunk > len(buf) {
			chunk = len(buf)
		}
		w.Write(buf[:chunk])
		written += chunk
	}
}

func (s *Server) emptyQueryResult(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
}

func (s *Server) itemCounts(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]int{
		"MovieCount":     0,
		"SeriesCount":    0,
		"EpisodeCount":   0,
		"ArtistCount":    0,
		"ProgramCount":   0,
		"TrailerCount":   0,
		"SongCount":      0,
		"AlbumCount":     0,
		"MusicVideoCount": 0,
		"BoxSetCount":    0,
		"BookCount":      0,
		"ItemCount":      0,
	})
}

func (s *Server) sessionsGet(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, []SessionInfo{})
}

func (s *Server) displayPreferences(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]any{
		"Id":              chi.URLParam(r, "id"),
		"SortBy":          "SortName",
		"SortOrder":       "Ascending",
		"RememberIndexing": false,
		"RememberSorting":  false,
		"Client":          s.extractClient(r),
		"CustomPrefs":     map[string]string{},
	})
}

func (s *Server) markPlayed(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, UserItemData{
		Played: r.Method == "POST",
		Key:    chi.URLParam(r, "itemId"),
	})
}

func (s *Server) markFavorite(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, UserItemData{
		IsFavorite: r.Method == "POST",
		Key:        chi.URLParam(r, "itemId"),
	})
}
