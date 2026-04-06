package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/handler"
	"github.com/gavinmcnair/tvproxy/pkg/logocache"
	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/openapi"
)

type routeHandlers struct {
	auth         *handler.AuthHandler
	user         *handler.UserHandler
	m3uAccount   *handler.M3UAccountHandler
	satip        *handler.SatIPHandler
	stream       *handler.StreamHandler
	channel      *handler.ChannelHandler
	channelGroup *handler.ChannelGroupHandler
	logo         *handler.LogoHandler
	profile      *handler.StreamProfileHandler
	epgSource    *handler.EPGSourceHandler
	epgData      *handler.EPGDataHandler
	hdhr         *handler.HDHRHandler
	output       *handler.OutputHandler
	proxy        *handler.ProxyHandler
	vod          *handler.VODHandler
	activity     *handler.ActivityHandler
	settings     *handler.SettingsHandler
	client       *handler.ClientHandler
	scheduler    *handler.SchedulerHandler
	dlna         *handler.DLNAHandler
	wireguard      *handler.WireGuardHandler
	wireguardMulti *handler.MultiWireGuardHandler
	tmdb           *handler.TMDBHandler
	logoCache      *logocache.Cache
	log            zerolog.Logger
}

func registerRoutes(r chi.Router, h routeHandlers, authMW *middleware.AuthMiddleware) {
	r.Get("/api/openapi.yaml", openapi.SpecHandler())
	r.Get("/api/docs", openapi.SwaggerUIHandler("/api/openapi.yaml"))

	r.Post("/api/auth/login", h.auth.Login)
	r.Post("/api/auth/refresh", h.auth.Refresh)
	r.Post("/api/auth/invite/{token}", h.auth.AcceptInvite)

	r.Get("/discover.json", h.hdhr.Discover)
	r.Get("/lineup_status.json", h.hdhr.LineupStatus)
	r.Get("/lineup.json", h.hdhr.Lineup)
	r.Get("/device.xml", h.hdhr.DeviceXML)
	r.Get("/capability", h.hdhr.DeviceXML)

	r.Get("/output/m3u", h.output.M3U)
	r.Get("/channels.m3u", h.output.M3U)
	r.Get("/channels.m3u8", h.output.M3U8)
	r.Get("/output/epg", h.output.EPG)

	r.Get("/dlna/device.xml", h.dlna.DeviceDescription)
	r.Get("/dlna/ContentDirectory.xml", h.dlna.ContentDirectorySCPD)
	r.Get("/dlna/ConnectionManager.xml", h.dlna.ConnectionManagerSCPD)
	r.Post("/dlna/control/ContentDirectory", h.dlna.ContentDirectoryControl)
	r.Post("/dlna/control/ConnectionManager", h.dlna.ConnectionManagerControl)

	r.Get("/channel/{channelID}", h.proxy.Stream)
	r.Head("/channel/{channelID}", h.proxy.StreamHead)
	r.Get("/stream/{streamID}", h.proxy.RawStream)
	r.Get("/recording/{streamID}/{filename}", h.vod.StreamRecordingDLNA)

	r.Get("/stream/{streamID}/probe", h.vod.ProbeStream)
	r.Post("/stream/{streamID}/vod", h.vod.CreateSession)
	r.Post("/channel/{channelID}/vod", h.vod.CreateChannelSession)
	r.Get("/vod/{sessionID}/status", h.vod.Status)
	r.Post("/vod/{sessionID}/seek", h.vod.Seek)
	r.Get("/vod/{sessionID}/stream", h.vod.Stream)
	r.Get("/vod/{sessionID}/dash/manifest.mpd", h.vod.DASHManifest)
	r.Get("/vod/{sessionID}/dash/{segment}", h.vod.DASHSegment)

	r.Group(func(r chi.Router) {
		r.Use(authMW.Authenticate)

		r.Delete("/vod/{sessionID}", h.vod.DeleteSession)
		r.Post("/api/vod/record/{channelID}", h.vod.StartRecording)
		r.Delete("/api/vod/record/{channelID}", h.vod.StopRecording)

		r.Post("/api/auth/logout", h.auth.Logout)
		r.Get("/api/auth/me", h.auth.Me)

		r.Route("/api/users", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", h.user.List)
			r.Post("/", h.user.Create)
			r.Post("/invite", h.user.Invite)
			r.Get("/{id}", h.user.Get)
			r.Put("/{id}", h.user.Update)
			r.Delete("/{id}", h.user.Delete)
		})

		r.Route("/api/m3u/accounts", func(r chi.Router) {
			r.Get("/", h.m3uAccount.List)
			r.Get("/{id}", h.m3uAccount.Get)
			r.Get("/{id}/status", h.m3uAccount.RefreshStatus)
			r.Group(func(r chi.Router) {
				r.Use(authMW.RequireAdmin)
				r.Post("/", h.m3uAccount.Create)
				r.Put("/{id}", h.m3uAccount.Update)
				r.Delete("/{id}", h.m3uAccount.Delete)
				r.Post("/{id}/refresh", h.m3uAccount.Refresh)
			})
		})

		r.Get("/api/satip/signal", h.satip.Signal)
		r.Get("/api/satip/transmitters", h.satip.ListTransmitters)
		r.Route("/api/satip/sources", func(r chi.Router) {
			r.Get("/", h.satip.List)
			r.Get("/{id}", h.satip.Get)
			r.Get("/{id}/status", h.satip.ScanStatus)
			r.Group(func(r chi.Router) {
				r.Use(authMW.RequireAdmin)
				r.Post("/", h.satip.Create)
				r.Put("/{id}", h.satip.Update)
				r.Delete("/{id}", h.satip.Delete)
				r.Post("/{id}/scan", h.satip.Scan)
				r.Post("/{id}/clear", h.satip.Clear)
			})
		})

		r.Route("/api/streams", func(r chi.Router) {
			r.Get("/", h.stream.List)
			r.Get("/{id}", h.stream.Get)
			r.Group(func(r chi.Router) {
				r.Use(authMW.RequireAdmin)
				r.Delete("/{id}", h.stream.Delete)
			})
		})

		r.Route("/api/channels", func(r chi.Router) {
			r.Get("/", h.channel.List)
			r.Post("/", h.channel.Create)
			r.Get("/{id}", h.channel.Get)
			r.Put("/{id}", h.channel.Update)
			r.Delete("/{id}", h.channel.Delete)
			r.Get("/{id}/streams", h.channel.GetStreams)
			r.Post("/{id}/streams", h.channel.AssignStreams)
			r.Post("/{id}/fail", h.channel.IncrementFailCount)
			r.Delete("/{id}/fail", h.channel.ResetFailCount)
		})

		r.Route("/api/channel-groups", func(r chi.Router) {
			r.Get("/", h.channelGroup.List)
			r.Post("/", h.channelGroup.Create)
			r.Get("/{id}", h.channelGroup.Get)
			r.Put("/{id}", h.channelGroup.Update)
			r.Delete("/{id}", h.channelGroup.Delete)
		})

		r.Route("/api/logos", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", h.logo.List)
			r.Post("/", h.logo.Create)
			r.Get("/{id}", h.logo.Get)
			r.Put("/{id}", h.logo.Update)
			r.Delete("/{id}", h.logo.Delete)
		})

		r.Route("/api/stream-profiles", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", h.profile.List)
			r.Post("/", h.profile.Create)
			r.Get("/{id}", h.profile.Get)
			r.Put("/{id}", h.profile.Update)
			r.Delete("/{id}", h.profile.Delete)
		})

		r.Route("/api/epg", func(r chi.Router) {
			r.Get("/sources", h.epgSource.List)
			r.Get("/sources/{id}", h.epgSource.Get)
			r.Get("/sources/{id}/status", h.epgSource.RefreshStatus)
			r.Get("/data", h.epgData.List)
			r.Get("/now", h.epgData.NowPlaying)
			r.Get("/guide", h.epgData.Guide)
			r.Group(func(r chi.Router) {
				r.Use(authMW.RequireAdmin)
				r.Post("/sources", h.epgSource.Create)
				r.Put("/sources/{id}", h.epgSource.Update)
				r.Delete("/sources/{id}", h.epgSource.Delete)
				r.Post("/sources/{id}/refresh", h.epgSource.Refresh)
			})
		})

		r.Route("/api/hdhr/devices", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", h.hdhr.ListDevices)
			r.Post("/", h.hdhr.CreateDevice)
			r.Get("/{id}", h.hdhr.GetDevice)
			r.Put("/{id}", h.hdhr.UpdateDevice)
			r.Delete("/{id}", h.hdhr.DeleteDevice)
		})

		r.Route("/api/settings", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", h.settings.List)
			r.Put("/", h.settings.Update)
			r.Get("/export", h.settings.Export)
			r.Post("/import", h.settings.Import)
			r.Get("/backup", h.settings.Backup)
			r.Post("/restore", h.settings.Restore)
			r.Post("/soft-reset", h.settings.SoftReset)
			r.Post("/hard-reset", h.settings.HardReset)
		})

		r.Route("/api/clients", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", h.client.List)
			r.Post("/", h.client.Create)
			r.Get("/{id}", h.client.Get)
			r.Put("/{id}", h.client.Update)
			r.Delete("/{id}", h.client.Delete)
		})

		r.Route("/api/recordings", func(r chi.Router) {
			r.Get("/completed", h.vod.ListCompletedRecordings)
			r.Get("/completed/{streamID}/{filename}/probe", h.vod.ProbeCompletedRecording)
			r.Get("/completed/{streamID}/{filename}/stream", h.vod.StreamCompletedRecording)
			r.Post("/completed/{streamID}/{filename}/play", h.vod.PlayCompletedRecording)
			r.Delete("/completed/{streamID}/{filename}", h.vod.DeleteCompletedRecording)
			r.Post("/schedule", h.scheduler.Schedule)
			r.Get("/schedule", h.scheduler.List)
			r.Get("/schedule/{id}", h.scheduler.Get)
			r.Delete("/schedule/{id}", h.scheduler.Delete)
		})

		r.Get("/api/tmdb/search", h.tmdb.Search)
		r.Get("/api/tmdb/details", h.tmdb.Details)
		r.Delete("/api/tmdb/cache", h.tmdb.InvalidateCache)
		r.Get("/api/vod/library", h.stream.VODLibrary)

		r.Route("/api/activity", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", h.activity.List)
		})

		r.Route("/api/wireguard", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/status", h.wireguard.Status)
			r.Post("/reconnect", h.wireguard.Reconnect)
			r.Post("/connect", h.wireguard.Connect)
			r.Post("/disconnect", h.wireguard.Disconnect)

			r.Get("/profiles", h.wireguardMulti.ListProfiles)
			r.Post("/profiles", h.wireguardMulti.CreateProfile)
			r.Get("/profiles/{id}", h.wireguardMulti.GetProfile)
			r.Put("/profiles/{id}", h.wireguardMulti.UpdateProfile)
			r.Delete("/profiles/{id}", h.wireguardMulti.DeleteProfile)
			r.Get("/profiles/{id}/status", h.wireguardMulti.ProfileStatus)
			r.Post("/profiles/{id}/reconnect", h.wireguardMulti.Reconnect)
			r.Post("/profiles/{id}/activate", h.wireguardMulti.SetActive)
			r.Get("/multi/status", h.wireguardMulti.Status)
		})
	})

	r.Get("/logo", h.logoCache.ServeHTTP)

	r.Post("/api/frontend-errors", frontendErrorHandler(h.log))
}

func frontendErrorHandler(log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Errors []struct {
				Msg    string `json:"msg"`
				Source string `json:"source"`
				Line   int    `json:"line"`
				Col    int    `json:"col"`
				Stack  string `json:"stack"`
			} `json:"errors"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, e := range body.Errors {
			log.Warn().Str("source", e.Source).Int("line", e.Line).Int("col", e.Col).Str("stack", e.Stack).Msg("frontend: " + e.Msg)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func registerStaticRoutes(r chi.Router, staticRoot string, distFS fs.FS, versionedIndexBytes []byte) {
	staticFileServer := http.FileServer(http.Dir(staticRoot))
	r.Handle("/static/*", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		staticFileServer.ServeHTTP(w, req)
	})))

	fileServer := http.FileServer(http.FS(distFS))
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/")
		if f, err := distFS.Open(path); err == nil {
			f.Close()
			if req.URL.RawQuery != "" && strings.Contains(req.URL.RawQuery, "v=") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
			fileServer.ServeHTTP(w, req)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(versionedIndexBytes)
	})
}
