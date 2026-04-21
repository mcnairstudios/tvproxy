package jellyfin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/hls"
	"github.com/gavinmcnair/tvproxy/pkg/service"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tmdb"
)

type Server struct {
	serverID        string
	serverName      string
	baseURL         string
	auth            *service.AuthService
	activityService *service.ActivityService
	favorites       store.FavoriteStore
	channels        store.ChannelStore
	channelGroups   store.ChannelGroupStore
	streams         store.StreamReader
	epg             store.EPGStore
	logoService     *service.LogoService
	tmdbClient      *tmdb.Client
	hlsManager      *hls.Manager
	WGProxyFunc     func(string) string
	log             zerolog.Logger
	tokens          sync.Map
}

func NewServer(serverName, baseURL string, auth *service.AuthService, activityService *service.ActivityService, favorites store.FavoriteStore, channels store.ChannelStore, channelGroups store.ChannelGroupStore, streams store.StreamReader, epg store.EPGStore, logoService *service.LogoService, tmdbClient *tmdb.Client, hlsManager *hls.Manager, log zerolog.Logger) *Server {
	return &Server{
		serverID:        generateGUID(),
		serverName:      serverName,
		baseURL:         baseURL,
		auth:            auth,
		activityService: activityService,
		favorites:       favorites,
		channels:        channels,
		channelGroups:   channelGroups,
		streams:         streams,
		epg:             epg,
		logoService:     logoService,
		tmdbClient:      tmdbClient,
		hlsManager:      hlsManager,
		log:             log.With().Str("component", "jellyfin").Logger(),
	}
}

func generateGUID() string {
	id := make([]byte, 16)
	rand.Read(id)
	h := hex.EncodeToString(id)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func (s *Server) Router() chi.Router {
	r := chi.NewRouter()

	s.registerWebRoutes(r)
	s.registerPublicRoutes(r)
	s.registerMediaRoutes(r)

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		s.registerUserRoutes(r)
		s.registerLibraryRoutes(r)
		s.registerLiveTvRoutes(r)
		s.registerSessionRoutes(r)
	})

	return r
}

func (s *Server) registerWebRoutes(r chi.Router) {
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
	r.Get("/web/{file}", s.webFile)
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
}

func (s *Server) registerPublicRoutes(r chi.Router) {
	r.Get("/System/Info/Public", s.systemInfoPublic)
	r.Get("/System/Info", s.systemInfo)
	r.Get("/System/Ping", s.ping)
	r.Post("/System/Ping", s.ping)
	r.Get("/Branding/Configuration", s.brandingConfig)
	r.Get("/Branding/Css", s.brandingCSS)
	r.Get("/Branding/Splashscreen", notFound)
	r.Get("/QuickConnect/Enabled", s.quickConnectEnabled)
	r.Get("/Users/Public", s.usersPublic)
	r.Post("/Users/AuthenticateByName", s.authenticateByName)
	r.Get("/UserImage", notFound)
	r.Head("/UserImage", notFound)
}

func (s *Server) registerMediaRoutes(r chi.Router) {
	r.Get("/Items/{itemId}/Images/{imageType}", s.serveImage)
	r.Get("/Items/{itemId}/Images/{imageType}/{imageIndex}", s.serveImage)
	r.Head("/Items/{itemId}/Images/{imageType}", s.serveImage)
	r.Head("/Items/{itemId}/Images/{imageType}/{imageIndex}", s.serveImage)
	r.Get("/Persons/{personId}/Images/{imageType}", s.servePersonImage)
	r.Get("/Videos/{itemId}/stream", s.videoStream)
	r.Get("/Videos/{itemId}/stream.{container}", s.videoStream)
	r.Head("/Videos/{itemId}/stream", s.videoStream)
	r.Head("/Videos/{itemId}/stream.{container}", s.videoStream)
	r.Get("/Videos/{itemId}/master.m3u8", s.hlsMasterPlaylist)
	r.Get("/Videos/{itemId}/main.m3u8", s.hlsMediaPlaylist)
	r.Get("/Videos/{itemId}/live.m3u8", s.hlsLivePlaylist)
	r.Get("/Videos/{itemId}/hls1/{playlistId}/{segment}", s.hlsSegment)
}

func (s *Server) registerUserRoutes(r chi.Router) {
	r.Get("/Users/Me", s.usersMe)
	r.Get("/Users", s.usersList)
	r.Get("/Users/{userId}", s.userByID)
	r.Post("/Users/Configuration", noContent)
	r.Post("/Users/Password", noContent)
}

func (s *Server) registerLibraryRoutes(r chi.Router) {
	r.Get("/UserViews", s.userViews)
	r.Get("/Items", s.listItems)
	r.Get("/Items/Filters", s.listFilters)
	r.Get("/Items/{itemId}", s.itemDetail)
	r.Get("/Items/Latest", s.latestItems)
	r.Get("/Items/Resume", s.listResumeItems)
	r.Get("/Items/Counts", s.itemCounts)
	r.Get("/Items/Suggestions", s.listSuggestions)
	r.Get("/UserItems/Resume", s.listResumeItems)
	r.Get("/Users/{userId}/Items", s.listItems)
	r.Get("/Users/{userId}/Items/Latest", s.latestItems)
	r.Get("/Users/{userId}/Items/Resume", s.listResumeItems)
	r.Get("/Users/{userId}/Items/{itemId}", s.itemDetail)
	r.Get("/Shows/NextUp", s.listResumeItems)
	r.Get("/Shows/{seriesId}/Seasons", s.listSeasons)
	r.Get("/Shows/{seriesId}/Episodes", s.listEpisodes)
	r.Get("/Items/{itemId}/Similar", s.listSimilarItems)
	r.Get("/Items/{itemId}/LocalTrailers", s.listSpecialFeatures)
	r.Get("/Items/{itemId}/SpecialFeatures", s.listSpecialFeatures)
	r.Get("/Items/{itemId}/ThemeMedia", s.listSpecialFeatures)
	r.Get("/Items/{itemId}/ThemeSongs", s.listSpecialFeatures)
	r.Get("/Items/{itemId}/ThemeVideos", s.listSpecialFeatures)
	r.Get("/Items/{itemId}/InstantMix", s.listSpecialFeatures)
	r.Post("/Items/{itemId}/PlaybackInfo", s.playbackInfo)
	r.Get("/Playback/BitrateTest", s.bitrateTest)

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
	r.Get("/Genres/{genreName}", func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "genreName")
		s.respondJSON(w, http.StatusOK, BaseItemDto{
			Name: name, ServerID: s.serverID,
			ID: fmt.Sprintf("genre_%x", hashString(name)), Type: "Genre",
		})
	})

	r.Post("/UserPlayedItems/{itemId}", s.markPlayed)
	r.Delete("/UserPlayedItems/{itemId}", s.markPlayed)
	r.Post("/UserFavoriteItems/{itemId}", s.markFavorite)
	r.Delete("/UserFavoriteItems/{itemId}", s.markFavorite)
	r.Get("/UserItems/{itemId}/UserData", s.getUserData)
	r.Post("/UserItems/{itemId}/UserData", noContent)
	r.Post("/UserItems/{itemId}/Rating", s.getUserData)
	r.Delete("/UserItems/{itemId}/Rating", s.getUserData)
}

func (s *Server) registerLiveTvRoutes(r chi.Router) {
	r.Get("/LiveTv/Info", s.liveTvInfo)
	r.Get("/LiveTv/Channels", s.liveTvChannels)
	r.Get("/LiveTv/Programs", s.liveTvPrograms)
	r.Get("/LiveTv/Programs/Recommended", s.liveTvPrograms)
	r.Post("/LiveTv/Programs", s.liveTvPrograms)
	r.Get("/LiveTv/GuideInfo", s.liveTvGuideInfo)
}

func (s *Server) registerSessionRoutes(r chi.Router) {
	r.Get("/Sessions", s.sessionsGet)
	r.Get("/System/ActivityLog/Entries", s.emptyQueryResult)
	r.Get("/Notifications/{userId}/Summary", func(w http.ResponseWriter, r *http.Request) {
		s.respondJSON(w, http.StatusOK, map[string]int{"UnreadCount": 0, "MaxUnreadCount": 0})
	})
	r.Post("/Sessions/Capabilities/Full", noContent)
	r.Post("/Sessions/Playing", noContent)
	r.Post("/Sessions/Playing/Progress", noContent)
	r.Post("/Sessions/Playing/Stopped", noContent)
	r.Get("/DisplayPreferences/{id}", s.displayPreferences)
	r.Post("/DisplayPreferences/{id}", noContent)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
}

func noContent(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8; profile=\"CamelCase\"")
	w.Header().Set("X-Application", "Jellyfin")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) emptyQueryResult(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, emptyResult())
}

func (s *Server) jellyfinBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host
}

func (s *Server) systemInfoPublic(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, PublicSystemInfo{
		LocalAddress:           s.jellyfinBaseURL(r),
		ServerName:             s.serverName,
		Version:                "10.10.6",
		ProductName:            "Jellyfin Server",
		OperatingSystem:        "Linux",
		ID:                     s.serverID,
		StartupWizardCompleted: true,
	})
}

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, SystemInfo{
		PublicSystemInfo: PublicSystemInfo{
			LocalAddress:           s.jellyfinBaseURL(r),
			ServerName:             s.serverName,
			Version:                "10.10.6",
			ProductName:            "Jellyfin Server",
			OperatingSystem:        "Linux",
			ID:                     s.serverID,
			StartupWizardCompleted: true,
		},
		OperatingSystemDisplayName: "Linux",
	})
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("Jellyfin Server"))
}

func (s *Server) brandingConfig(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BrandingConfiguration{})
}

func (s *Server) brandingCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
}

func (s *Server) quickConnectEnabled(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, false)
}

func (s *Server) webFile(w http.ResponseWriter, r *http.Request) {
	file := chi.URLParam(r, "file")
	switch file {
	case "config.json":
		s.respondJSON(w, http.StatusOK, map[string]any{
			"menuLinks": []any{}, "multiserver": false, "themes": []any{}, "plugins": []any{},
		})
	case "manifest.json":
		s.respondJSON(w, http.StatusOK, map[string]any{
			"name": "TVProxy", "short_name": "TVProxy", "start_url": "/web/index.html",
			"display": "standalone", "background_color": "#1a1d23", "theme_color": "#3b82f6",
		})
	default:
		w.WriteHeader(http.StatusOK)
	}
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
			Policy:                defaultPolicy(u.IsAdmin),
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

func (s *Server) usersMe(w http.ResponseWriter, r *http.Request) {
	userID := s.authenticatedUserID(r)
	user := s.lookupUser(r, userID)
	s.respondJSON(w, http.StatusOK, user)
}

func (s *Server) usersList(w http.ResponseWriter, r *http.Request) {
	userID := s.authenticatedUserID(r)
	user := s.lookupUser(r, userID)
	s.respondJSON(w, http.StatusOK, []UserDto{user})
}

func (s *Server) userByID(w http.ResponseWriter, r *http.Request) {
	s.usersMe(w, r)
}

func (s *Server) lookupUser(r *http.Request, userID string) UserDto {
	users, _ := s.auth.ListUsers(r.Context())
	name := "user"
	isAdmin := false
	for _, u := range users {
		if u.ID == userID {
			name = u.Username
			isAdmin = u.IsAdmin
			break
		}
	}
	now := time.Now()
	return UserDto{
		Name:                  name,
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
	}
}

func (s *Server) itemCounts(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]int{
		"MovieCount": 0, "SeriesCount": 0, "EpisodeCount": 0,
		"ArtistCount": 0, "ProgramCount": 0, "TrailerCount": 0,
		"SongCount": 0, "AlbumCount": 0, "MusicVideoCount": 0,
		"BoxSetCount": 0, "BookCount": 0, "ItemCount": 0,
	})
}

func (s *Server) sessionsGet(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, []SessionInfo{})
}

func (s *Server) getUserData(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, UserItemData{Key: chi.URLParam(r, "itemId")})
}

func (s *Server) displayPreferences(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]any{
		"Id":               chi.URLParam(r, "id"),
		"SortBy":           "SortName",
		"SortOrder":        "Ascending",
		"RememberIndexing": false,
		"RememberSorting":  false,
		"Client":           s.extractAuthField(r, "Client"),
		"CustomPrefs":      map[string]string{},
	})
}

func (s *Server) markPlayed(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, UserItemData{
		Played: r.Method == "POST",
		Key:    chi.URLParam(r, "itemId"),
	})
}

func (s *Server) markFavorite(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	userID := s.authenticatedUserID(r)
	isFav := r.Method == "POST"

	if s.favorites != nil && userID != "" {
		if isFav {
			s.favorites.Add(r.Context(), userID, addDashes(itemID))
		} else {
			s.favorites.Remove(r.Context(), userID, addDashes(itemID))
		}
	}

	s.respondJSON(w, http.StatusOK, UserItemData{
		IsFavorite: isFav,
		Key:        itemID,
	})
}

func (s *Server) bitrateTest(w http.ResponseWriter, r *http.Request) {
	size := 1000000
	if sizeStr := r.URL.Query().Get("size"); sizeStr != "" {
		if n, err := strconv.Atoi(sizeStr); err == nil && n > 0 && n <= 10000000 {
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
