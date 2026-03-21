package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type fullTestEnv struct {
	router      *chi.Mux
	authService *service.AuthService
	adminToken  string
	userToken   string
}

func setupFullEnv(t *testing.T) *fullTestEnv {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	db, err := database.New(context.Background(), dbPath, log)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	cfg := &config.Config{
		Host:               "localhost",
		Port:               8080,
		BaseURL:            "http://localhost",
		DatabasePath:       dbPath,
		JWTSecret:          "test-jwt-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		APIKey:             "test-api-key",
	}

	userRepo := repository.NewUserRepository(db)
	m3uAccountRepo := repository.NewM3UAccountRepository(db)
	streamRepo := repository.NewStreamRepository(db)
	channelRepo := repository.NewChannelRepository(db)
	channelGroupRepo := repository.NewChannelGroupRepository(db)
	logoRepo := repository.NewLogoRepository(db)
	streamProfileRepo := repository.NewStreamProfileRepository(db)
	epgSourceRepo := repository.NewEPGSourceRepository(db)
	epgDataRepo := repository.NewEPGDataRepository(db)
	programDataRepo := repository.NewProgramDataRepository(db)
	hdhrDeviceRepo := repository.NewHDHRDeviceRepository(db)
	settingsRepo := repository.NewCoreSettingsRepository(db)
	clientRepo := repository.NewClientRepository(db)

	authService := service.NewAuthService(userRepo, cfg.JWTSecret, cfg.AccessTokenExpiry, cfg.RefreshTokenExpiry)

	adminUser, err := authService.CreateUser(context.Background(), "admin", "adminpass", true)
	require.NoError(t, err)
	_, err = authService.CreateUser(context.Background(), "user", "userpass", false)
	require.NoError(t, err)
	adminUserID := adminUser.ID

	settingsService := service.NewSettingsService(settingsRepo)
	logoService := service.NewLogoService(logoRepo, settingsService, cfg, log)
	logoService.EnsureDir()

	m3uService := service.NewM3UService(m3uAccountRepo, streamRepo, logoService, cfg, log)
	channelService := service.NewChannelService(channelRepo, channelGroupRepo, streamRepo, log)
	epgService := service.NewEPGService(epgSourceRepo, epgDataRepo, programDataRepo, cfg, log)
	clientService := service.NewClientService(clientRepo, streamProfileRepo, log)
	proxyService := service.NewProxyService(channelRepo, streamRepo, streamProfileRepo, clientService, cfg, log)
	hdhrService := service.NewHDHRService(hdhrDeviceRepo, channelRepo)
	outputService := service.NewOutputService(channelRepo, channelGroupRepo, epgDataRepo, programDataRepo, logoService, cfg, log)
	ffmpegMgr := service.NewFFmpegManager(cfg, log)
	vodService := service.NewVODService(channelRepo, streamRepo, streamProfileRepo, ffmpegMgr, cfg, log)
	scheduledRecRepo := repository.NewScheduledRecordingRepository(db)
	schedulerService := service.NewSchedulerService(scheduledRecRepo, channelRepo, vodService, cfg, log)

	authMW := middleware.NewAuthMiddleware(authService, cfg.APIKey, adminUserID)

	authHandler := NewAuthHandler(authService)
	userHandler := NewUserHandler(authService)
	m3uAccountHandler := NewM3UAccountHandler(m3uService)
	streamHandler := NewStreamHandler(streamRepo, logoService)
	channelHandler := NewChannelHandler(channelService, logoService)
	channelGroupHandler := NewChannelGroupHandler(channelService)
	logoHandler := NewLogoHandler(logoService)
	streamProfileHandler := NewStreamProfileHandler(streamProfileRepo)
	epgSourceHandler := NewEPGSourceHandler(epgService)
	epgDataHandler := NewEPGDataHandler(epgDataRepo, programDataRepo)
	hdhrHandler := NewHDHRHandler(hdhrService, hdhrDeviceRepo, proxyService, cfg)
	outputHandler := NewOutputHandler(outputService)
	vodHandler := NewVODHandler(vodService, log)
	settingsHandler := NewSettingsHandler(settingsService, db, authService)
	clientHandler := NewClientHandler(clientService)
	schedulerHandler := NewSchedulerHandler(schedulerService, log)
	dlnaService := service.NewDLNAService(channelRepo, channelGroupRepo, settingsService, logoService, cfg, log)
	dlnaHandler := NewDLNAHandler(dlnaService, cfg)

	r := chi.NewRouter()

	r.Post("/api/auth/login", authHandler.Login)
	r.Post("/api/auth/refresh", authHandler.Refresh)
	r.Post("/api/auth/invite/{token}", authHandler.AcceptInvite)

	r.Get("/discover.json", hdhrHandler.Discover)
	r.Get("/lineup_status.json", hdhrHandler.LineupStatus)
	r.Get("/lineup.json", hdhrHandler.Lineup)
	r.Get("/device.xml", hdhrHandler.DeviceXML)

	r.Get("/output/m3u", outputHandler.M3U)
	r.Get("/output/epg", outputHandler.EPG)

	r.Get("/dlna/device.xml", dlnaHandler.DeviceDescription)
	r.Get("/dlna/ContentDirectory.xml", dlnaHandler.ContentDirectorySCPD)
	r.Get("/dlna/ConnectionManager.xml", dlnaHandler.ConnectionManagerSCPD)
	r.Post("/dlna/control/ContentDirectory", dlnaHandler.ContentDirectoryControl)
	r.Post("/dlna/control/ConnectionManager", dlnaHandler.ConnectionManagerControl)

	r.Get("/stream/{streamID}/probe", vodHandler.ProbeStream)
	r.Post("/stream/{streamID}/vod", vodHandler.CreateSession)
	r.Post("/channel/{channelID}/vod", vodHandler.CreateChannelSession)
	r.Get("/vod/{sessionID}/status", vodHandler.Status)
	r.Get("/vod/{sessionID}/seek", vodHandler.Seek)
	r.Get("/vod/{sessionID}/stream", vodHandler.Stream)

	r.Group(func(r chi.Router) {
		r.Use(authMW.Authenticate)

		r.Delete("/vod/{sessionID}", vodHandler.DeleteSession)
		r.Post("/vod/{sessionID}/record", vodHandler.MarkRecording)
		r.Put("/vod/{sessionID}/record/{segmentID}", vodHandler.UpdateSegment)
		r.Delete("/vod/{sessionID}/record/{segmentID}", vodHandler.DeleteSegment)
		r.Post("/vod/{sessionID}/stop", vodHandler.StopRecording)
		r.Post("/vod/{sessionID}/cancel", vodHandler.CancelRecording)
		r.Post("/channel/{channelID}/record", vodHandler.CreateRecording)

		r.Post("/api/auth/logout", authHandler.Logout)
		r.Get("/api/auth/me", authHandler.Me)

		r.Route("/api/users", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", userHandler.List)
			r.Post("/", userHandler.Create)
			r.Post("/invite", userHandler.Invite)
			r.Get("/{id}", userHandler.Get)
			r.Put("/{id}", userHandler.Update)
			r.Delete("/{id}", userHandler.Delete)
		})

		r.Route("/api/m3u/accounts", func(r chi.Router) {
			r.Get("/", m3uAccountHandler.List)
			r.Get("/{id}", m3uAccountHandler.Get)
			r.Group(func(r chi.Router) {
				r.Use(authMW.RequireAdmin)
				r.Post("/", m3uAccountHandler.Create)
				r.Put("/{id}", m3uAccountHandler.Update)
				r.Delete("/{id}", m3uAccountHandler.Delete)
				r.Post("/{id}/refresh", m3uAccountHandler.Refresh)
			})
		})

		r.Route("/api/streams", func(r chi.Router) {
			r.Get("/", streamHandler.List)
			r.Get("/{id}", streamHandler.Get)
			r.Group(func(r chi.Router) {
				r.Use(authMW.RequireAdmin)
				r.Delete("/{id}", streamHandler.Delete)
			})
		})

		r.Route("/api/channels", func(r chi.Router) {
			r.Get("/", channelHandler.List)
			r.Post("/", channelHandler.Create)
			r.Get("/{id}", channelHandler.Get)
			r.Put("/{id}", channelHandler.Update)
			r.Delete("/{id}", channelHandler.Delete)
			r.Get("/{id}/streams", channelHandler.GetStreams)
			r.Post("/{id}/streams", channelHandler.AssignStreams)
			r.Post("/{id}/fail", channelHandler.IncrementFailCount)
			r.Delete("/{id}/fail", channelHandler.ResetFailCount)
		})

		r.Route("/api/channel-groups", func(r chi.Router) {
			r.Get("/", channelGroupHandler.List)
			r.Post("/", channelGroupHandler.Create)
			r.Get("/{id}", channelGroupHandler.Get)
			r.Put("/{id}", channelGroupHandler.Update)
			r.Delete("/{id}", channelGroupHandler.Delete)
		})

		r.Route("/api/logos", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", logoHandler.List)
			r.Post("/", logoHandler.Create)
			r.Get("/{id}", logoHandler.Get)
			r.Put("/{id}", logoHandler.Update)
			r.Delete("/{id}", logoHandler.Delete)
		})

		r.Route("/api/stream-profiles", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", streamProfileHandler.List)
			r.Post("/", streamProfileHandler.Create)
			r.Get("/{id}", streamProfileHandler.Get)
			r.Put("/{id}", streamProfileHandler.Update)
			r.Delete("/{id}", streamProfileHandler.Delete)
		})

		r.Route("/api/epg", func(r chi.Router) {
			r.Get("/sources", epgSourceHandler.List)
			r.Get("/sources/{id}", epgSourceHandler.Get)
			r.Get("/data", epgDataHandler.List)
			r.Get("/now", epgDataHandler.NowPlaying)
			r.Get("/guide", epgDataHandler.Guide)
			r.Group(func(r chi.Router) {
				r.Use(authMW.RequireAdmin)
				r.Post("/sources", epgSourceHandler.Create)
				r.Put("/sources/{id}", epgSourceHandler.Update)
				r.Delete("/sources/{id}", epgSourceHandler.Delete)
				r.Post("/sources/{id}/refresh", epgSourceHandler.Refresh)
			})
		})

		r.Route("/api/hdhr/devices", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", hdhrHandler.ListDevices)
			r.Post("/", hdhrHandler.CreateDevice)
			r.Get("/{id}", hdhrHandler.GetDevice)
			r.Put("/{id}", hdhrHandler.UpdateDevice)
			r.Delete("/{id}", hdhrHandler.DeleteDevice)
		})

		r.Route("/api/settings", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", settingsHandler.List)
			r.Put("/", settingsHandler.Update)
			r.Post("/soft-reset", settingsHandler.SoftReset)
			r.Post("/hard-reset", settingsHandler.HardReset)
		})

		r.Route("/api/clients", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", clientHandler.List)
			r.Post("/", clientHandler.Create)
			r.Get("/{id}", clientHandler.Get)
			r.Put("/{id}", clientHandler.Update)
			r.Delete("/{id}", clientHandler.Delete)
		})

		r.Route("/api/recordings", func(r chi.Router) {
			r.Get("/", vodHandler.ListRecordings)
			r.Get("/completed", vodHandler.ListCompletedRecordings)
			r.Get("/completed/{filename}/stream", vodHandler.StreamCompletedRecording)
			r.Delete("/completed/{filename}", vodHandler.DeleteCompletedRecording)
			r.Post("/schedule", schedulerHandler.Schedule)
			r.Get("/schedule", schedulerHandler.List)
			r.Get("/schedule/{id}", schedulerHandler.Get)
			r.Delete("/schedule/{id}", schedulerHandler.Delete)
		})
	})

	env := &fullTestEnv{
		router:      r,
		authService: authService,
	}

	env.adminToken, _ = loginHelper(t, env, "admin", "adminpass")
	env.userToken, _ = loginHelper(t, env, "user", "userpass")

	return env
}

func loginHelper(t *testing.T, env *fullTestEnv, username, password string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp["access_token"], resp["refresh_token"]
}

func doRequest(t *testing.T, env *fullTestEnv, method, path string, body interface{}, token string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	require.NoError(t, json.NewDecoder(rec.Body).Decode(v))
}

func findByName(t *testing.T, env *fullTestEnv, endpoint, name, token string) map[string]interface{} {
	t.Helper()
	rec := doRequest(t, env, "GET", endpoint, nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var items []map[string]interface{}
	decodeResponse(t, rec, &items)
	for _, item := range items {
		if item["name"] == name {
			return item
		}
	}
	t.Fatalf("item with name %q not found in %s", name, endpoint)
	return nil
}

func TestIntegration_AuthFlow(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("login returns tokens", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/auth/login", map[string]string{
			"username": "admin", "password": "adminpass",
		}, "")
		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]string
		decodeResponse(t, rec, &resp)
		assert.NotEmpty(t, resp["access_token"])
		assert.NotEmpty(t, resp["refresh_token"])
	})

	t.Run("login bad password", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/auth/login", map[string]string{
			"username": "admin", "password": "wrong",
		}, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("login missing fields", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/auth/login", map[string]string{
			"username": "admin",
		}, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("me endpoint", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/auth/me", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]interface{}
		decodeResponse(t, rec, &resp)
		assert.Equal(t, "admin", resp["username"])
		assert.Equal(t, true, resp["is_admin"])
	})

	t.Run("me without auth", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/auth/me", nil, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("me with API key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/me", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]interface{}
		decodeResponse(t, rec, &resp)
		assert.Equal(t, "api-key", resp["username"])
		assert.Equal(t, true, resp["is_admin"])
		assert.NotEmpty(t, resp["user_id"])
	})

	t.Run("me with wrong API key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/me", nil)
		req.Header.Set("X-API-Key", "wrong-key")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("refresh token", func(t *testing.T) {
		_, refreshToken := loginHelper(t, env, "admin", "adminpass")
		rec := doRequest(t, env, "POST", "/api/auth/refresh", map[string]string{
			"refresh_token": refreshToken,
		}, "")
		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]string
		decodeResponse(t, rec, &resp)
		assert.NotEmpty(t, resp["access_token"])
	})

	t.Run("refresh invalid token", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/auth/refresh", map[string]string{
			"refresh_token": "invalid",
		}, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("logout", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/auth/logout", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestIntegration_UserCRUD(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("list users as admin", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/users/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var users []map[string]interface{}
		decodeResponse(t, rec, &users)
		assert.Len(t, users, 2)
	})

	t.Run("list users as non-admin is forbidden", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/users/", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("create user", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/users/", map[string]interface{}{
			"username": "newuser", "password": "newpass", "is_admin": false,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var user map[string]interface{}
		decodeResponse(t, rec, &user)
		assert.Equal(t, "newuser", user["username"])
		assert.Equal(t, false, user["is_admin"])
		assert.NotZero(t, user["id"])
	})

	t.Run("create user missing fields", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/users/", map[string]interface{}{
			"username": "nopass",
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("get user by id", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/users/", nil, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var users []map[string]interface{}
		decodeResponse(t, rec, &users)
		var adminID string
		for _, u := range users {
			if u["username"] == "admin" {
				adminID = u["id"].(string)
				break
			}
		}
		require.NotEmpty(t, adminID)

		rec = doRequest(t, env, "GET", "/api/users/"+adminID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var user map[string]interface{}
		decodeResponse(t, rec, &user)
		assert.Equal(t, "admin", user["username"])
	})

	t.Run("get non-existent user", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/users/00000000-0000-0000-0000-000000000000", nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update user", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/users/", nil, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var users []map[string]interface{}
		decodeResponse(t, rec, &users)
		var userID string
		for _, u := range users {
			if u["username"] == "user" {
				userID = u["id"].(string)
				break
			}
		}
		require.NotEmpty(t, userID)

		rec = doRequest(t, env, "PUT", "/api/users/"+userID, map[string]interface{}{
			"username": "updateduser", "is_admin": false,
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var user map[string]interface{}
		decodeResponse(t, rec, &user)
		assert.Equal(t, "updateduser", user["username"])
	})

	t.Run("delete user", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/users/", map[string]interface{}{
			"username": "todelete", "password": "pass",
		}, env.adminToken)
		require.Equal(t, http.StatusCreated, rec.Code)
		var user map[string]interface{}
		decodeResponse(t, rec, &user)
		id := user["id"].(string)

		rec = doRequest(t, env, "DELETE", "/api/users/"+id, nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		rec = doRequest(t, env, "GET", "/api/users/"+id, nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_ChannelGroupCRUD(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("list empty", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channel-groups/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var groups []map[string]interface{}
		decodeResponse(t, rec, &groups)
		assert.Len(t, groups, 0)
	})

	var groupID string

	t.Run("create", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
			"name": "Sports", "is_enabled": true, "sort_order": 1,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var group map[string]interface{}
		decodeResponse(t, rec, &group)
		assert.Equal(t, "Sports", group["name"])
		assert.Equal(t, true, group["is_enabled"])
		assert.Equal(t, float64(1), group["sort_order"])
		groupID = group["id"].(string)
	})

	t.Run("create missing name", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
			"is_enabled": true,
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("get by id", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channel-groups/"+groupID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var group map[string]interface{}
		decodeResponse(t, rec, &group)
		assert.Equal(t, "Sports", group["name"])
	})

	t.Run("update", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/channel-groups/"+groupID, map[string]interface{}{
			"name": "Sports HD", "is_enabled": true, "sort_order": 2,
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var group map[string]interface{}
		decodeResponse(t, rec, &group)
		assert.Equal(t, "Sports HD", group["name"])
		assert.Equal(t, float64(2), group["sort_order"])
	})

	t.Run("list after create", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channel-groups/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var groups []map[string]interface{}
		decodeResponse(t, rec, &groups)
		assert.Len(t, groups, 1)
	})

	t.Run("delete", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
			"name": "ToDelete", "is_enabled": false,
		}, env.adminToken)
		require.Equal(t, http.StatusCreated, rec.Code)
		var g map[string]interface{}
		decodeResponse(t, rec, &g)
		id := g["id"].(string)

		rec = doRequest(t, env, "DELETE", "/api/channel-groups/"+id, nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		rec = doRequest(t, env, "GET", "/api/channel-groups/"+id, nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("get non-existent", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channel-groups/00000000-0000-0000-0000-000000000000", nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_StreamProfileCRUD(t *testing.T) {
	env := setupFullEnv(t)

	var createdProfileID1 string
	var createdProfileID2 string

	t.Run("create with dropdowns", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
			"name": "SAT>IP QSV H264", "stream_mode": "ffmpeg", "source_type": "satip", "hwaccel": "qsv", "video_codec": "h264", "is_default": false,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var profile map[string]interface{}
		decodeResponse(t, rec, &profile)
		assert.Equal(t, "SAT>IP QSV H264", profile["name"])
		assert.Equal(t, "ffmpeg", profile["stream_mode"])
		assert.Equal(t, "satip", profile["source_type"])
		assert.Equal(t, "qsv", profile["hwaccel"])
		assert.Equal(t, "h264", profile["video_codec"])
		assert.Equal(t, "ffmpeg", profile["command"])
		assert.Contains(t, profile["args"], "h264_qsv")
		assert.Nil(t, profile["custom_args"])
		createdProfileID1 = profile["id"].(string)
	})

	t.Run("create with custom args", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
			"name": "Custom", "source_type": "m3u", "hwaccel": "none", "video_codec": "copy",
			"custom_args": "-b:v 4M", "is_default": false,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var profile map[string]interface{}
		decodeResponse(t, rec, &profile)
		assert.Equal(t, "ffmpeg", profile["command"])
		assert.Equal(t, "-b:v 4M", profile["custom_args"])
		assert.Contains(t, profile["args"], "-b:v 4M")
		assert.Contains(t, profile["args"], "-i {input}")
		createdProfileID2 = profile["id"].(string)
	})

	t.Run("create with use_custom_args", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
			"name": "Full Custom", "source_type": "m3u", "hwaccel": "none", "video_codec": "copy",
			"use_custom_args": true,
			"custom_args":     "-i {input} -c:v copy -c:a copy -f mpegts pipe:1",
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var profile map[string]interface{}
		decodeResponse(t, rec, &profile)
		assert.Equal(t, true, profile["use_custom_args"])
		assert.Equal(t, "-i {input} -c:v copy -c:a copy -f mpegts pipe:1", profile["args"])
		assert.Equal(t, "-i {input} -c:v copy -c:a copy -f mpegts pipe:1", profile["custom_args"])
	})

	t.Run("create duplicate name returns 409", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
			"name": "Browser", "source_type": "m3u", "hwaccel": "none", "video_codec": "copy",
		}, env.adminToken)
		assert.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("list includes seeded defaults", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/stream-profiles/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var profiles []map[string]interface{}
		decodeResponse(t, rec, &profiles)
		assert.Len(t, profiles, 14)
	})

	t.Run("get", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/stream-profiles/"+createdProfileID1, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var profile map[string]interface{}
		decodeResponse(t, rec, &profile)
		assert.Equal(t, "SAT>IP QSV H264", profile["name"])
		assert.Equal(t, "mpegts", profile["container"])
	})

	t.Run("update", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/stream-profiles/"+createdProfileID1, map[string]interface{}{
			"name": "SAT>IP NVENC AV1", "source_type": "satip", "hwaccel": "nvenc", "video_codec": "av1", "is_default": false,
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var profile map[string]interface{}
		decodeResponse(t, rec, &profile)
		assert.Equal(t, "SAT>IP NVENC AV1", profile["name"])
		assert.Equal(t, "nvenc", profile["hwaccel"])
		assert.Equal(t, "av1", profile["video_codec"])
		assert.Contains(t, profile["args"], "av1_nvenc")
	})

	t.Run("delete non-system profile", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/stream-profiles/"+createdProfileID2, nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("delete system profile is forbidden", func(t *testing.T) {
		directProfile := findByName(t, env, "/api/stream-profiles/", "Direct", env.adminToken)
		directID := directProfile["id"].(string)
		rec := doRequest(t, env, "DELETE", "/api/stream-profiles/"+directID, nil, env.adminToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("proxy profile is system and default", func(t *testing.T) {
		proxyProfile := findByName(t, env, "/api/stream-profiles/", "Proxy", env.adminToken)
		proxyID := proxyProfile["id"].(string)
		rec := doRequest(t, env, "GET", "/api/stream-profiles/"+proxyID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var profile map[string]interface{}
		decodeResponse(t, rec, &profile)
		assert.Equal(t, "Proxy", profile["name"])
		assert.Equal(t, true, profile["is_system"])
		assert.Equal(t, true, profile["is_default"])
		assert.Equal(t, false, profile["is_client"])
	})

	t.Run("browser client profile is client not system", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/stream-profiles/", nil, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var profiles []map[string]interface{}
		decodeResponse(t, rec, &profiles)
		var browserClientID string
		for _, p := range profiles {
			if p["name"] == "Browser (Client)" && p["is_client"] == true {
				browserClientID = p["id"].(string)
				break
			}
		}
		require.NotEmpty(t, browserClientID)

		rec = doRequest(t, env, "GET", "/api/stream-profiles/"+browserClientID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var profile map[string]interface{}
		decodeResponse(t, rec, &profile)
		assert.Equal(t, "Browser (Client)", profile["name"])
		assert.Equal(t, false, profile["is_system"])
		assert.Equal(t, true, profile["is_client"])
	})

	t.Run("delete client profile is forbidden", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/stream-profiles/", nil, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var profiles []map[string]interface{}
		decodeResponse(t, rec, &profiles)
		var plexClientProfileID string
		for _, p := range profiles {
			if p["name"] == "Plex (Client)" && p["is_client"] == true {
				plexClientProfileID = p["id"].(string)
				break
			}
		}
		require.NotEmpty(t, plexClientProfileID)

		rec = doRequest(t, env, "DELETE", "/api/stream-profiles/"+plexClientProfileID, nil, env.adminToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("update system profile is forbidden", func(t *testing.T) {
		directProfile := findByName(t, env, "/api/stream-profiles/", "Direct", env.adminToken)
		directID := directProfile["id"].(string)
		rec := doRequest(t, env, "PUT", "/api/stream-profiles/"+directID, map[string]interface{}{
			"name": "Renamed Direct",
		}, env.adminToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("update client profile is allowed", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/stream-profiles/", nil, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var profiles []map[string]interface{}
		decodeResponse(t, rec, &profiles)
		var plexClientProfileID string
		for _, p := range profiles {
			if p["name"] == "Plex (Client)" && p["is_client"] == true {
				plexClientProfileID = p["id"].(string)
				break
			}
		}
		require.NotEmpty(t, plexClientProfileID)

		rec = doRequest(t, env, "PUT", "/api/stream-profiles/"+plexClientProfileID, map[string]interface{}{
			"name": "Plex Custom", "source_type": "m3u", "hwaccel": "qsv", "video_codec": "h264",
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var profile map[string]interface{}
		decodeResponse(t, rec, &profile)
		assert.Equal(t, "Plex Custom", profile["name"])
		assert.Equal(t, "qsv", profile["hwaccel"])
		assert.Equal(t, true, profile["is_client"])
	})

	t.Run("list ordering: system first, then client, then regular", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/stream-profiles/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var profiles []map[string]interface{}
		decodeResponse(t, rec, &profiles)
		assert.Equal(t, true, profiles[0]["is_system"])
		assert.Equal(t, true, profiles[1]["is_system"])
		foundClient := false
		for i := 2; i < len(profiles); i++ {
			if profiles[i]["is_client"] == true {
				foundClient = true
			}
			if profiles[i]["is_system"] != true && profiles[i]["is_client"] != true && foundClient {
				for j := i + 1; j < len(profiles); j++ {
					assert.NotEqual(t, true, profiles[j]["is_client"], "client profile found after regular profile")
				}
				break
			}
		}
	})

	t.Run("create missing name", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
			"source_type": "m3u",
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create invalid source_type", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
			"name": "Bad", "source_type": "invalid",
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create invalid stream_mode", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
			"name": "Bad Mode", "stream_mode": "invalid",
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create with direct stream_mode", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
			"name": "My Direct", "stream_mode": "direct", "is_default": false,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var profile map[string]interface{}
		decodeResponse(t, rec, &profile)
		assert.Equal(t, "direct", profile["stream_mode"])
	})

	t.Run("default stream_mode is ffmpeg", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
			"name": "No Mode Specified", "source_type": "m3u", "is_default": false,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var profile map[string]interface{}
		decodeResponse(t, rec, &profile)
		assert.Equal(t, "ffmpeg", profile["stream_mode"])
	})
}

func TestIntegration_LogoCRUD(t *testing.T) {
	env := setupFullEnv(t)

	var logoID string

	t.Run("create", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/logos/", map[string]interface{}{
			"name": "BBC Logo", "url": "https://example.com/bbc.png",
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var logo map[string]interface{}
		decodeResponse(t, rec, &logo)
		assert.Equal(t, "BBC Logo", logo["name"])
		assert.Equal(t, "https://example.com/bbc.png", logo["url"])
		logoID = logo["id"].(string)
	})

	t.Run("create missing fields", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/logos/", map[string]interface{}{
			"name": "NoURL",
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("list", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/logos/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var logos []map[string]interface{}
		decodeResponse(t, rec, &logos)
		assert.Len(t, logos, 1)
	})

	t.Run("get", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/logos/"+logoID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get non-existent", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/logos/00000000-0000-0000-0000-000000000000", nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("delete", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/logos/"+logoID, nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestIntegration_LogoChannelPropagation(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "POST", "/api/logos/", map[string]interface{}{
		"name": "BBC Logo", "url": "https://example.com/bbc.png",
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)
	var logo map[string]interface{}
	decodeResponse(t, rec, &logo)
	logoID := logo["id"].(string)

	rec = doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "BBC One", "logo_id": logoID, "is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)
	var ch map[string]interface{}
	decodeResponse(t, rec, &ch)
	assert.Equal(t, logoID, ch["logo_id"])
	assert.Equal(t, "https://example.com/bbc.png", ch["logo"])

	channelID := ch["id"].(string)

	rec = doRequest(t, env, "PUT", "/api/logos/"+logoID, map[string]interface{}{
		"name": "BBC Logo", "url": "https://example.com/bbc-hd.png",
	}, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = doRequest(t, env, "GET", "/api/channels/"+channelID, nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)
	decodeResponse(t, rec, &ch)
	assert.Equal(t, "https://example.com/bbc-hd.png", ch["logo"])

	rec = doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "ITV", "logo": "https://example.com/itv.png", "is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)
	decodeResponse(t, rec, &ch)
	assert.NotNil(t, ch["logo_id"])
	assert.Equal(t, "https://example.com/itv.png", ch["logo"])

	rec = doRequest(t, env, "GET", "/api/logos/", nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var logos []map[string]interface{}
	decodeResponse(t, rec, &logos)
	assert.Len(t, logos, 2)

	rec = doRequest(t, env, "DELETE", "/api/logos/"+logoID, nil, env.adminToken)
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = doRequest(t, env, "GET", "/api/channels/"+channelID, nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var chAfterDelete map[string]interface{}
	decodeResponse(t, rec, &chAfterDelete)
	assert.Nil(t, chAfterDelete["logo_id"])
	assert.Contains(t, chAfterDelete["logo"], "data:image/svg+xml")
}

func TestIntegration_ChannelFailCount(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "Fail Test Channel", "is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)
	var ch map[string]interface{}
	decodeResponse(t, rec, &ch)
	channelID := ch["id"].(string)
	assert.Equal(t, float64(0), ch["fail_count"])

	rec = doRequest(t, env, "POST", "/api/channels/"+channelID+"/fail", nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)
	decodeResponse(t, rec, &ch)
	assert.Equal(t, float64(1), ch["fail_count"])

	rec = doRequest(t, env, "POST", "/api/channels/"+channelID+"/fail", nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)
	decodeResponse(t, rec, &ch)
	assert.Equal(t, float64(2), ch["fail_count"])

	rec = doRequest(t, env, "DELETE", "/api/channels/"+channelID+"/fail", nil, env.adminToken)
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = doRequest(t, env, "GET", "/api/channels/"+channelID, nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)
	decodeResponse(t, rec, &ch)
	assert.Equal(t, float64(0), ch["fail_count"])
}

func TestIntegration_Settings(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("list empty", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/settings/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var settings []map[string]interface{}
		decodeResponse(t, rec, &settings)
		assert.Len(t, settings, 0)
	})

	t.Run("update settings", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/settings/", map[string]string{
			"app_name":   "TVProxy Test",
			"base_url":   "http://localhost:8080",
			"epg_source": "xmltv",
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("list after update", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/settings/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var settings []map[string]interface{}
		decodeResponse(t, rec, &settings)
		assert.Len(t, settings, 3)

		settingsMap := make(map[string]string)
		for _, s := range settings {
			settingsMap[s["key"].(string)] = s["value"].(string)
		}
		assert.Equal(t, "TVProxy Test", settingsMap["app_name"])
		assert.Equal(t, "http://localhost:8080", settingsMap["base_url"])
		assert.Equal(t, "xmltv", settingsMap["epg_source"])
	})

	t.Run("overwrite setting", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/settings/", map[string]string{
			"app_name": "TVProxy Updated",
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)

		rec = doRequest(t, env, "GET", "/api/settings/", nil, env.adminToken)
		var settings []map[string]interface{}
		decodeResponse(t, rec, &settings)
		settingsMap := make(map[string]string)
		for _, s := range settings {
			settingsMap[s["key"].(string)] = s["value"].(string)
		}
		assert.Equal(t, "TVProxy Updated", settingsMap["app_name"])
	})
}

func TestIntegration_M3UAccountCRUD(t *testing.T) {
	env := setupFullEnv(t)

	var accountID string

	t.Run("list empty", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/m3u/accounts/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var accounts []map[string]interface{}
		decodeResponse(t, rec, &accounts)
		assert.Len(t, accounts, 0)
	})

	t.Run("create", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/m3u/accounts/", map[string]interface{}{
			"name":             "Test M3U",
			"url":              "http://example.com/playlist.m3u",
			"type":             "m3u",
			"max_streams":      5,
			"is_enabled":       true,
			"refresh_interval": 3600,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var account map[string]interface{}
		decodeResponse(t, rec, &account)
		assert.Equal(t, "Test M3U", account["name"])
		assert.Equal(t, "http://example.com/playlist.m3u", account["url"])
		assert.Equal(t, "m3u", account["type"])
		assert.Equal(t, float64(5), account["max_streams"])
		assert.Equal(t, true, account["is_enabled"])
		accountID = account["id"].(string)
	})

	t.Run("create missing fields", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/m3u/accounts/", map[string]interface{}{
			"name": "NoURL",
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("get", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/m3u/accounts/"+accountID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var account map[string]interface{}
		decodeResponse(t, rec, &account)
		assert.Equal(t, "Test M3U", account["name"])
	})

	t.Run("get non-existent", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/m3u/accounts/00000000-0000-0000-0000-000000000000", nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/m3u/accounts/"+accountID, map[string]interface{}{
			"name":             "Updated M3U",
			"url":              "http://example.com/new.m3u",
			"type":             "m3u",
			"max_streams":      10,
			"is_enabled":       false,
			"refresh_interval": 7200,
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var account map[string]interface{}
		decodeResponse(t, rec, &account)
		assert.Equal(t, "Updated M3U", account["name"])
		assert.Equal(t, float64(10), account["max_streams"])
		assert.Equal(t, false, account["is_enabled"])
	})

	var xtreamID string

	t.Run("create xtream account", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/m3u/accounts/", map[string]interface{}{
			"name":       "Xtream",
			"url":        "http://example.com:8080",
			"type":       "xtream",
			"username":   "testuser",
			"password":   "testpass",
			"is_enabled": true,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var account map[string]interface{}
		decodeResponse(t, rec, &account)
		assert.Equal(t, "xtream", account["type"])
		assert.Equal(t, "testuser", account["username"])
		xtreamID = account["id"].(string)
	})

	t.Run("list after creates", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/m3u/accounts/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var accounts []map[string]interface{}
		decodeResponse(t, rec, &accounts)
		assert.Len(t, accounts, 2)
	})

	t.Run("delete", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/m3u/accounts/"+xtreamID, nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		rec = doRequest(t, env, "GET", "/api/m3u/accounts/"+xtreamID, nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_XtreamRefresh(t *testing.T) {
	env := setupFullEnv(t)

	xtreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("action")
		username := r.URL.Query().Get("username")
		password := r.URL.Query().Get("password")

		if username != "testuser" || password != "testpass" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch action {
		case "get_live_streams":
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"num": 1, "name": "ESPN HD", "stream_type": "live", "stream_id": 101, "stream_icon": "http://example.com/espn.png", "epg_channel_id": "espn.us", "category_id": "1", "category_name": "Sports"},
				{"num": 2, "name": "CNN", "stream_type": "live", "stream_id": 102, "stream_icon": "http://example.com/cnn.png", "epg_channel_id": "cnn.us", "category_id": "2", "category_name": "News"},
			})
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"user_info":   map[string]interface{}{"username": "testuser", "status": "Active", "max_connections": "2", "active_cons": "0"},
				"server_info": map[string]interface{}{"url": "example.com", "port": "8080"},
			})
		}
	}))
	defer xtreamServer.Close()

	var accountID string

	t.Run("create xtream account", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/m3u/accounts/", map[string]interface{}{
			"name":       "Xtream Test",
			"url":        xtreamServer.URL,
			"type":       "xtream",
			"username":   "testuser",
			"password":   "testpass",
			"is_enabled": true,
		}, env.adminToken)
		require.Equal(t, http.StatusCreated, rec.Code)
		var account map[string]interface{}
		decodeResponse(t, rec, &account)
		accountID = account["id"].(string)
	})

	t.Run("refresh populates streams", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/m3u/accounts/"+accountID+"/refresh", nil, env.adminToken)
		assert.Equal(t, http.StatusAccepted, rec.Code)

		require.Eventually(t, func() bool {
			rec := doRequest(t, env, "GET", "/api/streams/?full=true", nil, env.adminToken)
			var streams []map[string]interface{}
			json.NewDecoder(rec.Body).Decode(&streams)
			return len(streams) == 2
		}, 5*time.Second, 100*time.Millisecond)

		rec = doRequest(t, env, "GET", "/api/streams/?full=true", nil, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var streams []map[string]interface{}
		decodeResponse(t, rec, &streams)
		require.Len(t, streams, 2)

		names := map[string]bool{}
		for _, s := range streams {
			names[s["name"].(string)] = true
			assert.Equal(t, accountID, s["m3u_account_id"])
			if s["name"] == "ESPN HD" {
				assert.Equal(t, "Sports", s["group"])
				assert.Equal(t, "espn.us", s["tvg_id"])
				assert.Contains(t, s["url"].(string), "/101.ts")
			}
		}
		assert.True(t, names["ESPN HD"])
		assert.True(t, names["CNN"])
	})

	t.Run("account updated after refresh", func(t *testing.T) {
		require.Eventually(t, func() bool {
			rec := doRequest(t, env, "GET", "/api/m3u/accounts/"+accountID, nil, env.adminToken)
			var account map[string]interface{}
			json.NewDecoder(rec.Body).Decode(&account)
			return account["stream_count"] == float64(2)
		}, 5*time.Second, 100*time.Millisecond)

		rec := doRequest(t, env, "GET", "/api/m3u/accounts/"+accountID, nil, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var account map[string]interface{}
		decodeResponse(t, rec, &account)
		assert.Equal(t, float64(2), account["stream_count"])
		assert.NotNil(t, account["last_refreshed"])
		assert.Equal(t, "", account["last_error"])
	})

	t.Run("refresh with bad credentials sets last_error", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/m3u/accounts/"+accountID, map[string]interface{}{
			"name":       "Xtream Test",
			"url":        xtreamServer.URL,
			"type":       "xtream",
			"username":   "baduser",
			"password":   "badpass",
			"is_enabled": true,
		}, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)

		rec = doRequest(t, env, "POST", "/api/m3u/accounts/"+accountID+"/refresh", nil, env.adminToken)
		assert.Equal(t, http.StatusAccepted, rec.Code)

		require.Eventually(t, func() bool {
			rec := doRequest(t, env, "GET", "/api/m3u/accounts/"+accountID, nil, env.adminToken)
			var account map[string]interface{}
			json.NewDecoder(rec.Body).Decode(&account)
			lastError, _ := account["last_error"].(string)
			return lastError != ""
		}, 5*time.Second, 100*time.Millisecond)
	})
}

func TestIntegration_EPGSourceCRUD(t *testing.T) {
	env := setupFullEnv(t)

	var sourceID string

	t.Run("create", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/epg/sources", map[string]interface{}{
			"name": "Test EPG", "url": "http://example.com/epg.xml", "is_enabled": true,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var source map[string]interface{}
		decodeResponse(t, rec, &source)
		assert.Equal(t, "Test EPG", source["name"])
		assert.Equal(t, true, source["is_enabled"])
		assert.Equal(t, float64(0), source["channel_count"])
		assert.Equal(t, float64(0), source["program_count"])
		sourceID = source["id"].(string)
	})

	t.Run("create missing fields", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/epg/sources", map[string]interface{}{
			"name": "NoURL",
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("get", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/epg/sources/"+sourceID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var source map[string]interface{}
		decodeResponse(t, rec, &source)
		assert.Equal(t, "Test EPG", source["name"])
	})

	t.Run("update", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/epg/sources/"+sourceID, map[string]interface{}{
			"name": "Updated EPG", "url": "http://example.com/new-epg.xml", "is_enabled": false,
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var source map[string]interface{}
		decodeResponse(t, rec, &source)
		assert.Equal(t, "Updated EPG", source["name"])
	})

	t.Run("list", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/epg/sources", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var sources []map[string]interface{}
		decodeResponse(t, rec, &sources)
		assert.Len(t, sources, 1)
	})

	t.Run("delete", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/epg/sources/"+sourceID, nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		rec = doRequest(t, env, "GET", "/api/epg/sources/"+sourceID, nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_EPGData(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("list empty", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/epg/data", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var data []map[string]interface{}
		decodeResponse(t, rec, &data)
		assert.Len(t, data, 0)
	})

	t.Run("list with source_id filter", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/epg/data?source_id=00000000-0000-0000-0000-000000000001", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var data []map[string]interface{}
		decodeResponse(t, rec, &data)
		assert.Len(t, data, 0)
	})

	t.Run("source_id string returns empty results", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/epg/data?source_id=abc", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var data []map[string]interface{}
		decodeResponse(t, rec, &data)
		assert.Len(t, data, 0)
	})
}

func TestIntegration_HDHRDeviceCRUD(t *testing.T) {
	env := setupFullEnv(t)

	var firstDeviceID string

	t.Run("list empty", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/hdhr/devices/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var devices []map[string]interface{}
		decodeResponse(t, rec, &devices)
		assert.Len(t, devices, 0)
	})

	t.Run("create", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/hdhr/devices/", map[string]interface{}{
			"name":             "TVProxy HDHR",
			"device_id":        "12345678",
			"device_auth":      "test-auth",
			"firmware_version": "20200101",
			"tuner_count":      2,
			"is_enabled":       true,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var device map[string]interface{}
		decodeResponse(t, rec, &device)
		assert.Equal(t, "TVProxy HDHR", device["name"])
		assert.Equal(t, "12345678", device["device_id"])
		assert.Equal(t, float64(2), device["tuner_count"])
		assert.Equal(t, float64(47601), device["port"])
		firstDeviceID = device["id"].(string)
	})

	t.Run("create with channel groups", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
			"name": "Sports", "is_enabled": true,
		}, env.adminToken)
		require.Equal(t, http.StatusCreated, rec.Code)
		var g1 map[string]interface{}
		decodeResponse(t, rec, &g1)

		rec = doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
			"name": "News", "is_enabled": true,
		}, env.adminToken)
		require.Equal(t, http.StatusCreated, rec.Code)
		var g2 map[string]interface{}
		decodeResponse(t, rec, &g2)

		rec = doRequest(t, env, "POST", "/api/hdhr/devices/", map[string]interface{}{
			"name": "Multi-Group HDHR", "device_id": "MULTI123", "device_auth": "auth",
			"tuner_count": 2, "is_enabled": true,
			"channel_group_ids": []interface{}{g1["id"].(string), g2["id"].(string)},
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var device map[string]interface{}
		decodeResponse(t, rec, &device)
		assert.NotNil(t, device["channel_group_ids"])
		groupIDs := device["channel_group_ids"].([]interface{})
		assert.Len(t, groupIDs, 2)

		id := device["id"].(string)
		rec = doRequest(t, env, "GET", "/api/hdhr/devices/"+id, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		decodeResponse(t, rec, &device)
		groupIDs = device["channel_group_ids"].([]interface{})
		assert.Len(t, groupIDs, 2)
	})

	t.Run("create missing fields", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/hdhr/devices/", map[string]interface{}{
			"name": "Missing DeviceID",
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("get", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/hdhr/devices/"+firstDeviceID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("update", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/hdhr/devices/"+firstDeviceID, map[string]interface{}{
			"name":              "Updated HDHR",
			"device_id":         "12345678",
			"firmware_version":  "20240101",
			"tuner_count":       4,
			"port":              47605,
			"is_enabled":        true,
			"channel_group_ids": []interface{}{},
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var device map[string]interface{}
		decodeResponse(t, rec, &device)
		assert.Equal(t, "Updated HDHR", device["name"])
		assert.Equal(t, float64(4), device["tuner_count"])
		assert.Equal(t, float64(47605), device["port"])
	})

	t.Run("list after create", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/hdhr/devices/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var devices []map[string]interface{}
		decodeResponse(t, rec, &devices)
		assert.Len(t, devices, 2)
	})

	t.Run("delete", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/hdhr/devices/"+firstDeviceID, nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestIntegration_HDHRDiscovery(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("lineup status", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/lineup_status.json", nil, "")
		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]interface{}
		decodeResponse(t, rec, &resp)
		assert.Equal(t, "Cable", resp["Source"])
	})

	t.Run("discover without devices fails", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/discover.json", nil, "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("discover with device", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/hdhr/devices/", map[string]interface{}{
			"name": "Test HDHR", "device_id": "ABCD1234", "device_auth": "auth",
			"firmware_version": "20240101", "tuner_count": 2, "port": 47601, "is_enabled": true,
			"channel_group_ids": []interface{}{},
		}, env.adminToken)
		require.Equal(t, http.StatusCreated, rec.Code)

		rec = doRequest(t, env, "GET", "/discover.json", nil, "")
		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]interface{}
		decodeResponse(t, rec, &resp)
		assert.Equal(t, "Test HDHR", resp["FriendlyName"])
		assert.Equal(t, "ABCD1234", resp["DeviceID"])
		assert.Equal(t, float64(2), resp["TunerCount"])
	})

	t.Run("lineup empty", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/lineup.json", nil, "")
		assert.Equal(t, http.StatusOK, rec.Code)
		var lineup []map[string]interface{}
		decodeResponse(t, rec, &lineup)
		assert.Len(t, lineup, 0)
	})

	t.Run("device xml", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/device.xml", nil, "")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))
	})
}

func TestIntegration_Streams(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("list empty summaries", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/streams/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var streams []map[string]interface{}
		decodeResponse(t, rec, &streams)
		assert.Len(t, streams, 0)
	})

	t.Run("list empty full", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/streams/?full=true", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var streams []map[string]interface{}
		decodeResponse(t, rec, &streams)
		assert.Len(t, streams, 0)
	})

	t.Run("list by account_id", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/streams/?account_id=00000000-0000-0000-0000-000000000001", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var streams []map[string]interface{}
		decodeResponse(t, rec, &streams)
		assert.Len(t, streams, 0)
	})

	t.Run("account_id string returns empty results", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/streams/?account_id=abc", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var streams []map[string]interface{}
		decodeResponse(t, rec, &streams)
		assert.Len(t, streams, 0)
	})

	t.Run("get non-existent", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/streams/00000000-0000-0000-0000-000000000000", nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_ChannelCRUD(t *testing.T) {
	env := setupFullEnv(t)

	var firstChannelID string

	t.Run("list empty", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channels/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var channels []map[string]interface{}
		decodeResponse(t, rec, &channels)
		assert.Len(t, channels, 0)
	})

	t.Run("create", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
			"name": "BBC One", "tvg_id": "bbc1", "is_enabled": true,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var ch map[string]interface{}
		decodeResponse(t, rec, &ch)
		assert.Equal(t, "BBC One", ch["name"])
		assert.Equal(t, true, ch["is_enabled"])
		firstChannelID = ch["id"].(string)
	})

	t.Run("create second channel", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
			"name": "BBC Two", "tvg_id": "bbc2", "is_enabled": true,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("create with channel group", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
			"name": "News", "is_enabled": true,
		}, env.adminToken)
		require.Equal(t, http.StatusCreated, rec.Code)
		var group map[string]interface{}
		decodeResponse(t, rec, &group)
		groupID := group["id"].(string)

		rec = doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
			"name": "Sky News", "channel_group_id": groupID, "is_enabled": true,
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var ch map[string]interface{}
		decodeResponse(t, rec, &ch)
		assert.Equal(t, groupID, ch["channel_group_id"])
	})

	t.Run("create missing name", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
			"tvg_id": "test",
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("get", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channels/"+firstChannelID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var ch map[string]interface{}
		decodeResponse(t, rec, &ch)
		assert.Equal(t, "BBC One", ch["name"])
	})

	t.Run("get non-existent", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channels/00000000-0000-0000-0000-000000000000", nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/channels/"+firstChannelID, map[string]interface{}{
			"name": "BBC One HD", "tvg_id": "bbc1hd", "is_enabled": true,
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var ch map[string]interface{}
		decodeResponse(t, rec, &ch)
		assert.Equal(t, "BBC One HD", ch["name"])
		assert.Equal(t, "bbc1hd", ch["tvg_id"])
	})

	t.Run("list after creates", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channels/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var channels []map[string]interface{}
		decodeResponse(t, rec, &channels)
		assert.Len(t, channels, 3)
	})

	t.Run("delete", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
			"name": "ToDelete", "is_enabled": true,
		}, env.adminToken)
		require.Equal(t, http.StatusCreated, rec.Code)
		var ch map[string]interface{}
		decodeResponse(t, rec, &ch)
		deleteID := ch["id"].(string)

		rec = doRequest(t, env, "DELETE", "/api/channels/"+deleteID, nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		rec = doRequest(t, env, "GET", "/api/channels/"+deleteID, nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_Output(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("m3u empty", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/output/m3u", nil, "")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "audio/x-mpegurl", rec.Header().Get("Content-Type"))
		assert.Contains(t, rec.Body.String(), "#EXTM3U")
	})

	t.Run("epg empty", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/output/epg", nil, "")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))
		assert.Contains(t, rec.Body.String(), `<?xml version="1.0"`)
		assert.Contains(t, rec.Body.String(), `<tv generator-info-name="tvproxy">`)
		assert.Contains(t, rec.Body.String(), `</tv>`)
	})

	t.Run("m3u with channels", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
			"name": "Entertainment", "is_enabled": true, "sort_order": 1,
		}, env.adminToken)
		require.Equal(t, http.StatusCreated, rec.Code)
		var group map[string]interface{}
		decodeResponse(t, rec, &group)
		groupID := group["id"].(string)

		doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
			"name": "Channel One", "tvg_id": "ch1",
			"channel_group_id": groupID, "is_enabled": true,
			"logo": "https://example.com/logo.png",
		}, env.adminToken)

		doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
			"name": "Channel Two", "is_enabled": false,
		}, env.adminToken)

		rec = doRequest(t, env, "GET", "/output/m3u", nil, "")
		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, "#EXTM3U")
		assert.Contains(t, body, "Channel One")
		assert.Contains(t, body, `tvg-id="ch1"`)
		assert.Contains(t, body, `tvg-logo="https://example.com/logo.png"`)
		assert.Contains(t, body, `group-title="Entertainment"`)
		assert.NotContains(t, body, "Channel Two")
	})
}

func TestIntegration_FullUserWorkflow(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "GET", "/api/auth/me", nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = doRequest(t, env, "POST", "/api/users/", map[string]interface{}{
		"username": "operator", "password": "oppass", "is_admin": false,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	operatorToken, _ := loginHelper(t, env, "operator", "oppass")

	rec = doRequest(t, env, "POST", "/api/m3u/accounts/", map[string]interface{}{
		"name": "Primary IPTV", "url": "http://iptv.example.com/get.php?type=m3u_plus",
		"type": "m3u", "max_streams": 2, "is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
		"name": "Sports", "is_enabled": true, "sort_order": 1,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)
	var sportsGroup map[string]interface{}
	decodeResponse(t, rec, &sportsGroup)
	sportsGroupID := sportsGroup["id"].(string)

	rec = doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
		"name": "Movies", "is_enabled": true, "sort_order": 2,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)
	var moviesGroup map[string]interface{}
	decodeResponse(t, rec, &moviesGroup)
	moviesGroupID := moviesGroup["id"].(string)

	rec = doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
		"name": "My Direct Profile", "stream_mode": "direct", "is_default": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "Sky Sports 1", "tvg_id": "skysports1", "channel_group_id": sportsGroupID,
		"is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "HBO", "tvg_id": "hbo", "channel_group_id": moviesGroupID,
		"is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "POST", "/api/epg/sources", map[string]interface{}{
		"name": "EPG Source", "url": "http://epg.example.com/xmltv.xml", "is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "POST", "/api/hdhr/devices/", map[string]interface{}{
		"name": "TVProxy Tuner", "device_id": "ABCDEF12", "device_auth": "auth123",
		"firmware_version": "20240101", "tuner_count": 4, "port": 47601, "is_enabled": true,
		"channel_group_ids": []interface{}{sportsGroupID, moviesGroupID},
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "PUT", "/api/settings/", map[string]string{
		"base_url": "http://myserver:8080",
	}, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = doRequest(t, env, "POST", "/api/logos/", map[string]interface{}{
		"name": "Sky Sports Logo", "url": "https://example.com/sky-sports.png",
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "GET", "/api/channels/", nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var channels []map[string]interface{}
	decodeResponse(t, rec, &channels)
	assert.Len(t, channels, 2)

	rec = doRequest(t, env, "GET", "/output/m3u", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	m3uBody := rec.Body.String()
	assert.Contains(t, m3uBody, "Sky Sports 1")
	assert.Contains(t, m3uBody, "HBO")
	assert.Contains(t, m3uBody, `group-title="Sports"`)
	assert.Contains(t, m3uBody, `group-title="Movies"`)

	rec = doRequest(t, env, "GET", "/discover.json", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	var discover map[string]interface{}
	decodeResponse(t, rec, &discover)
	assert.Equal(t, "TVProxy Tuner", discover["FriendlyName"])

	rec = doRequest(t, env, "GET", "/lineup.json", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	var lineup []map[string]interface{}
	decodeResponse(t, rec, &lineup)
	assert.Len(t, lineup, 2)

	rec = doRequest(t, env, "GET", "/output/epg", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `<tv generator-info-name="tvproxy">`)

	rec = doRequest(t, env, "GET", "/api/settings/", nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var settings []map[string]interface{}
	decodeResponse(t, rec, &settings)
	assert.Len(t, settings, 1)

	rec = doRequest(t, env, "GET", "/api/users/", nil, operatorToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = doRequest(t, env, "GET", "/api/channels/", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, env, "GET", "/api/m3u/accounts/", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, env, "GET", "/output/m3u", nil, "")
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = doRequest(t, env, "GET", "/lineup_status.json", nil, "")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestIntegration_NonAdminAccess(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("non-admin can list channels", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channels/", nil, env.userToken)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("non-admin can create channels", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
			"name": "User Channel", "is_enabled": true,
		}, env.userToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("non-admin can list streams", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/streams/", nil, env.userToken)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("non-admin can list epg sources", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/epg/sources", nil, env.userToken)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("non-admin can create channel group", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
			"name": "User Group", "is_enabled": true,
		}, env.userToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("non-admin cannot list users", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/users/", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot create users", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/users/", map[string]interface{}{
			"username": "hacker", "password": "pass",
		}, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot delete users", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/users/", nil, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var users []map[string]interface{}
		decodeResponse(t, rec, &users)
		adminID := users[0]["id"].(string)

		rec = doRequest(t, env, "DELETE", "/api/users/"+adminID, nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot create m3u accounts", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/m3u/accounts/", map[string]interface{}{
			"name": "Hacker M3U", "url": "http://evil.com/m3u", "type": "m3u",
		}, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot create stream profiles", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/stream-profiles/", map[string]interface{}{
			"name": "Hacker Profile", "source_type": "m3u",
		}, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot list stream profiles", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/stream-profiles/", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot create hdhr devices", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/hdhr/devices/", map[string]interface{}{
			"name": "Hacker HDHR", "device_id": "HACK1234", "device_auth": "auth",
			"tuner_count": 1, "is_enabled": true,
		}, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot list hdhr devices", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/hdhr/devices/", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot create logos", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/logos/", map[string]interface{}{
			"name": "Hacker Logo", "url": "http://evil.com/logo.png",
		}, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot list logos", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/logos/", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot update settings", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/settings/", map[string]string{
			"base_url": "http://hacked",
		}, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot list settings", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/settings/", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot create clients", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/clients/", map[string]interface{}{
			"name": "Hacker Client", "priority": 1,
			"match_rules": []map[string]interface{}{
				{"header_name": "User-Agent", "match_type": "contains", "match_value": "hack"},
			},
		}, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot list clients", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/clients/", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot create EPG sources", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/epg/sources", map[string]interface{}{
			"name": "Hacker EPG", "url": "http://evil.com/epg.xml", "is_enabled": true,
		}, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot delete EPG sources", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/epg/sources/00000000-0000-0000-0000-000000000000", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin can read EPG sources", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/epg/sources", nil, env.userToken)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("non-admin cannot delete streams", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/streams/00000000-0000-0000-0000-000000000000", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin cannot invite users", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/users/invite", map[string]interface{}{
			"username": "sneaky",
		}, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestIntegration_EdgeCases(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("invalid JSON body", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/channels/", bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+env.adminToken)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("non-existent id returns not found", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channels/notanumber", nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/auth/me", nil, "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjF9.invalid")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("nil slice responses are empty arrays", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/channels/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "[]")
	})

	t.Run("update non-existent channel group", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/channel-groups/00000000-0000-0000-0000-000000000000", map[string]interface{}{
			"name": "Ghost",
		}, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update non-existent channel", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/channels/00000000-0000-0000-0000-000000000000", map[string]interface{}{
			"name": "Ghost",
		}, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update non-existent m3u account", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/m3u/accounts/00000000-0000-0000-0000-000000000000", map[string]interface{}{
			"name": "Ghost",
		}, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update non-existent stream profile", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/stream-profiles/00000000-0000-0000-0000-000000000000", map[string]interface{}{
			"name": "Ghost",
		}, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update non-existent epg source", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/epg/sources/00000000-0000-0000-0000-000000000000", map[string]interface{}{
			"name": "Ghost", "url": "http://x",
		}, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update non-existent hdhr device", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/hdhr/devices/00000000-0000-0000-0000-000000000000", map[string]interface{}{
			"name": "Ghost", "device_id": "x",
		}, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update non-existent user", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/users/00000000-0000-0000-0000-000000000000", map[string]interface{}{
			"username": "Ghost",
		}, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update non-existent client", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/api/clients/00000000-0000-0000-0000-000000000000", map[string]interface{}{
			"name": "Ghost",
		}, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_ClientCRUD(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("list seeded clients", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/clients/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var clients []map[string]interface{}
		decodeResponse(t, rec, &clients)
		assert.Len(t, clients, 4)
		assert.Equal(t, "Plex", clients[0]["name"])
		assert.Equal(t, "VLC", clients[1]["name"])
		assert.Equal(t, "Quest", clients[2]["name"])
		assert.Equal(t, "Browser", clients[3]["name"])
	})

	t.Run("get seeded client with rules", func(t *testing.T) {
		plexClient := findByName(t, env, "/api/clients/", "Plex", env.adminToken)
		plexID := plexClient["id"].(string)

		rec := doRequest(t, env, "GET", "/api/clients/"+plexID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var client map[string]interface{}
		decodeResponse(t, rec, &client)
		assert.Equal(t, "Plex", client["name"])
		assert.Equal(t, float64(10), client["priority"])
		assert.Equal(t, true, client["is_enabled"])
		rules := client["match_rules"].([]interface{})
		assert.Len(t, rules, 2)
	})

	var createdClientID string

	t.Run("create with auto profile", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/clients/", map[string]interface{}{
			"name": "Oculus Quest", "priority": 15, "is_enabled": true,
			"match_rules": []map[string]interface{}{
				{"header_name": "User-Agent", "match_type": "contains", "match_value": "OculusBrowser/"},
			},
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var client map[string]interface{}
		decodeResponse(t, rec, &client)
		assert.Equal(t, "Oculus Quest", client["name"])
		assert.Equal(t, float64(15), client["priority"])
		assert.NotZero(t, client["stream_profile_id"])
		rules := client["match_rules"].([]interface{})
		assert.Len(t, rules, 1)
		createdClientID = client["id"].(string)
	})

	t.Run("create missing name", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/clients/", map[string]interface{}{
			"priority": 50,
			"match_rules": []map[string]interface{}{
				{"header_name": "User-Agent", "match_type": "contains", "match_value": "test"},
			},
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create missing rules", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/clients/", map[string]interface{}{
			"name": "Bad Client", "priority": 50,
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create with invalid match_type", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/clients/", map[string]interface{}{
			"name": "Bad Match", "priority": 50,
			"match_rules": []map[string]interface{}{
				{"header_name": "User-Agent", "match_type": "regex", "match_value": ".*"},
			},
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create with missing match_value for non-exists", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/clients/", map[string]interface{}{
			"name": "Missing Value", "priority": 50,
			"match_rules": []map[string]interface{}{
				{"header_name": "User-Agent", "match_type": "contains"},
			},
		}, env.adminToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("update client", func(t *testing.T) {
		newPriority := 25
		rec := doRequest(t, env, "PUT", "/api/clients/"+createdClientID, map[string]interface{}{
			"name": "Oculus Quest 2", "priority": newPriority,
			"match_rules": []map[string]interface{}{
				{"header_name": "User-Agent", "match_type": "contains", "match_value": "OculusBrowser/"},
				{"header_name": "Accept", "match_type": "exists"},
			},
		}, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var client map[string]interface{}
		decodeResponse(t, rec, &client)
		assert.Equal(t, "Oculus Quest 2", client["name"])
		assert.Equal(t, float64(25), client["priority"])
		rules := client["match_rules"].([]interface{})
		assert.Len(t, rules, 2)
	})

	t.Run("delete client cleans up profile", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/clients/"+createdClientID, nil, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var client map[string]interface{}
		decodeResponse(t, rec, &client)
		profileID := client["stream_profile_id"].(string)

		rec = doRequest(t, env, "DELETE", "/api/clients/"+createdClientID, nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		rec = doRequest(t, env, "GET", "/api/clients/"+createdClientID, nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)

		rec = doRequest(t, env, "GET", "/api/stream-profiles/"+profileID, nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("list after create and delete", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/clients/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var clients []map[string]interface{}
		decodeResponse(t, rec, &clients)
		assert.Len(t, clients, 4)
	})
}

func TestIntegration_ChannelIsolation(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "Admin Channel", "is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "User Channel", "is_enabled": true,
	}, env.userToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "GET", "/api/channels/", nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var adminChannels []map[string]interface{}
	decodeResponse(t, rec, &adminChannels)
	assert.Len(t, adminChannels, 1)
	assert.Equal(t, "Admin Channel", adminChannels[0]["name"])

	rec = doRequest(t, env, "GET", "/api/channels/", nil, env.userToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var userChannels []map[string]interface{}
	decodeResponse(t, rec, &userChannels)
	assert.Len(t, userChannels, 1)
	assert.Equal(t, "User Channel", userChannels[0]["name"])

	adminChannelID := adminChannels[0]["id"].(string)
	rec = doRequest(t, env, "GET", "/api/channels/"+adminChannelID, nil, env.userToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
		"name": "Admin Group", "is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
		"name": "User Group", "is_enabled": true,
	}, env.userToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "GET", "/api/channel-groups/", nil, env.adminToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var adminGroups []map[string]interface{}
	decodeResponse(t, rec, &adminGroups)
	assert.Len(t, adminGroups, 1)
	assert.Equal(t, "Admin Group", adminGroups[0]["name"])

	rec = doRequest(t, env, "GET", "/api/channel-groups/", nil, env.userToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var userGroups []map[string]interface{}
	decodeResponse(t, rec, &userGroups)
	assert.Len(t, userGroups, 1)
	assert.Equal(t, "User Group", userGroups[0]["name"])
}

func TestIntegration_InviteFlow(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "POST", "/api/users/invite", map[string]interface{}{
		"username": "invited_user",
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)
	var inviteResp map[string]interface{}
	decodeResponse(t, rec, &inviteResp)
	assert.Equal(t, "invited_user", inviteResp["username"])
	assert.NotNil(t, inviteResp["invite_token"])
	token := inviteResp["invite_token"].(string)

	rec = doRequest(t, env, "POST", "/api/auth/login", map[string]string{
		"username": "invited_user", "password": "newpass",
	}, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, env, "POST", "/api/auth/invite/"+token, map[string]interface{}{
		"password": "newpass",
	}, "")
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = doRequest(t, env, "POST", "/api/auth/login", map[string]string{
		"username": "invited_user", "password": "newpass",
	}, "")
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = doRequest(t, env, "POST", "/api/users/invite", map[string]interface{}{
		"username": "another_user",
	}, env.userToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestIntegration_SoftReset(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("non-admin denied", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/settings/soft-reset", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("unauthenticated denied", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/settings/soft-reset", nil, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("admin succeeds", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/stream-profiles/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var profiles []map[string]interface{}
		decodeResponse(t, rec, &profiles)
		assert.Greater(t, len(profiles), 0)

		rec = doRequest(t, env, "POST", "/api/settings/soft-reset", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)

		rec = doRequest(t, env, "GET", "/api/stream-profiles/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		decodeResponse(t, rec, &profiles)
		assert.Equal(t, 10, len(profiles))

		rec = doRequest(t, env, "GET", "/api/channels/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var channels []map[string]interface{}
		decodeResponse(t, rec, &channels)
		assert.Len(t, channels, 0)

		rec = doRequest(t, env, "GET", "/api/users/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var users []map[string]interface{}
		decodeResponse(t, rec, &users)
		assert.Equal(t, 2, len(users))
	})
}

func TestIntegration_HardReset(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("non-admin denied", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/settings/hard-reset", nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("admin succeeds", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/settings/hard-reset", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)

		newToken, _ := loginHelper(t, env, "admin", "admin")

		rec = doRequest(t, env, "GET", "/api/stream-profiles/", nil, newToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var profiles []map[string]interface{}
		decodeResponse(t, rec, &profiles)
		assert.Equal(t, 11, len(profiles))

		rec = doRequest(t, env, "GET", "/api/clients/", nil, newToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var clients []map[string]interface{}
		decodeResponse(t, rec, &clients)
		assert.Equal(t, 4, len(clients))

		rec = doRequest(t, env, "GET", "/api/users/", nil, newToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var users []map[string]interface{}
		decodeResponse(t, rec, &users)
		assert.Equal(t, 1, len(users))
		assert.Equal(t, "admin", users[0]["username"])
	})
}

func TestIntegration_VODDeleteSessionRequiresAuth(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("unauthenticated delete returns 401", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/vod/some-session", nil, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("authenticated delete on nonexistent returns 204", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/vod/nonexistent-session", nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestIntegration_VODStatusNotFound(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "GET", "/vod/nonexistent/status", nil, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestIntegration_VODSeekNotFound(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "GET", "/vod/nonexistent/seek?t=0", nil, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestIntegration_VODStreamNotFound(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "GET", "/vod/nonexistent/stream", nil, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestIntegration_VODMarkRecordingRequiresAuth(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/vod/some-session/record", map[string]string{
			"program_title": "test",
		}, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("authenticated on nonexistent session returns 404", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/vod/nonexistent/record", map[string]interface{}{
			"program_title": "test",
			"channel_name":  "ch1",
		}, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_VODStopRecordingRequiresAuth(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/vod/some-session/stop", nil, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("nonexistent session returns 404", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/vod/nonexistent/stop", nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_VODCancelRecordingRequiresAuth(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/vod/some-session/cancel", nil, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("nonexistent session returns 404", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/vod/nonexistent/cancel", nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_VODUpdateSegmentRequiresAuth(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/vod/some-session/record/some-seg", map[string]interface{}{
			"start_offset": 5.0,
		}, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("nonexistent session returns 404", func(t *testing.T) {
		rec := doRequest(t, env, "PUT", "/vod/nonexistent/record/some-seg", map[string]interface{}{
			"start_offset": 5.0,
		}, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_VODDeleteSegmentRequiresAuth(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/vod/some-session/record/some-seg", nil, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("nonexistent session returns 404", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/vod/nonexistent/record/some-seg", nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_VODRecordingsListRequiresAuth(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/recordings/", nil, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("authenticated returns empty list", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/recordings/", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var list []interface{}
		decodeResponse(t, rec, &list)
		assert.Empty(t, list)
	})
}

func TestIntegration_VODCompletedRecordingsRequiresAuth(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/recordings/completed", nil, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("authenticated returns empty list", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/recordings/completed", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var list []interface{}
		decodeResponse(t, rec, &list)
		assert.Empty(t, list)
	})
}

func TestIntegration_VODDeleteCompletedRecordingRequiresAuth(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/recordings/completed/test.mp4", nil, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid filename returns 400", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/recordings/completed/..%2Fetc%2Fpasswd", nil, env.adminToken)
		// chi decodes URL params, so ../etc/passwd → invalid filename
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusNotFound}, rec.Code)
	})
}

func TestIntegration_VODCreateSessionRequiresStreamID(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("nonexistent stream returns 404", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/stream/nonexistent-stream/vod", nil, "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_VODCreateChannelSessionNotFound(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "POST", "/channel/nonexistent-channel/vod", nil, "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestIntegration_VODCreateRecordingRequiresAuth(t *testing.T) {
	env := setupFullEnv(t)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/channel/some-channel/record", map[string]interface{}{
			"program_title": "test",
		}, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestIntegration_HDHR_Discover(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "POST", "/api/hdhr/devices/", map[string]interface{}{
		"name": "Test Tuner", "device_id": "AABBCCDD", "device_auth": "authkey123",
		"firmware_version": "20240101", "tuner_count": 4, "port": 47601, "is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "GET", "/discover.json", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var discover map[string]interface{}
	decodeResponse(t, rec, &discover)
	assert.Equal(t, "Test Tuner", discover["FriendlyName"])
	assert.Equal(t, "Silicondust", discover["Manufacturer"])
	assert.Equal(t, "https://www.silicondust.com/", discover["ManufacturerURL"])
	assert.Equal(t, "HDTC-2US", discover["ModelNumber"])
	assert.Equal(t, "hdhomerun_atsc", discover["FirmwareName"])
	assert.Equal(t, "20240101", discover["FirmwareVersion"])
	assert.Equal(t, "AABBCCDD", discover["DeviceID"])
	assert.Equal(t, "authkey123", discover["DeviceAuth"])
	assert.NotEmpty(t, discover["BaseURL"])
	assert.Contains(t, discover["LineupURL"].(string), "/lineup.json")
	assert.Equal(t, float64(4), discover["TunerCount"])
}

func TestIntegration_HDHR_Lineup(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "POST", "/api/hdhr/devices/", map[string]interface{}{
		"name": "Lineup Tuner", "device_id": "11223344", "device_auth": "auth",
		"firmware_version": "20240101", "tuner_count": 2, "port": 47602, "is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "CH 1", "is_enabled": true,
	}, env.adminToken)
	doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "CH 2", "is_enabled": true,
	}, env.adminToken)
	doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "CH Disabled", "is_enabled": false,
	}, env.adminToken)

	rec = doRequest(t, env, "GET", "/lineup.json", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var lineup []map[string]interface{}
	decodeResponse(t, rec, &lineup)
	require.Len(t, lineup, 2)

	assert.Equal(t, "1", lineup[0]["GuideNumber"])
	assert.Equal(t, "CH 1", lineup[0]["GuideName"])
	assert.NotEmpty(t, lineup[0]["URL"])

	assert.Equal(t, "2", lineup[1]["GuideNumber"])
	assert.Equal(t, "CH 2", lineup[1]["GuideName"])
	assert.NotEmpty(t, lineup[1]["URL"])
}

func TestIntegration_HDHR_LineupStatus(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "GET", "/lineup_status.json", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var status map[string]interface{}
	decodeResponse(t, rec, &status)
	assert.Equal(t, float64(0), status["ScanInProgress"])
	assert.Equal(t, float64(1), status["ScanPossible"])
	assert.Equal(t, "Cable", status["Source"])
	sourceList := status["SourceList"].([]interface{})
	require.Len(t, sourceList, 1)
	assert.Equal(t, "Cable", sourceList[0])
}

func TestIntegration_HDHR_DeviceXML(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "POST", "/api/hdhr/devices/", map[string]interface{}{
		"name": "XML Tuner", "device_id": "DEADBEEF", "device_auth": "xmlauth",
		"firmware_version": "20240101", "tuner_count": 3, "port": 47603, "is_enabled": true,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "GET", "/device.xml", nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))

	body := rec.Body.String()
	assert.Contains(t, body, "urn:schemas-upnp-org:device-1-0")
	assert.Contains(t, body, "urn:schemas-upnp-org:device:MediaServer:1")
	assert.Contains(t, body, "XML Tuner")
	assert.Contains(t, body, "Silicondust")
	assert.Contains(t, body, "HDTC-2US")
	assert.Contains(t, body, "DEADBEEF")
	assert.Contains(t, body, "uuid:DEADBEEF")
}

func TestIntegration_HDHR_GroupFiltering(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
		"name": "Sports", "is_enabled": true, "sort_order": 1,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)
	var sportsGroup map[string]interface{}
	decodeResponse(t, rec, &sportsGroup)
	sportsGroupID := sportsGroup["id"].(string)

	rec = doRequest(t, env, "POST", "/api/channel-groups/", map[string]interface{}{
		"name": "Movies", "is_enabled": true, "sort_order": 2,
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)

	doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "ESPN", "channel_group_id": sportsGroupID, "is_enabled": true,
	}, env.adminToken)
	doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "Sky Sports", "channel_group_id": sportsGroupID, "is_enabled": true,
	}, env.adminToken)
	doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": "HBO", "is_enabled": true,
	}, env.adminToken)

	rec = doRequest(t, env, "POST", "/api/hdhr/devices/", map[string]interface{}{
		"name": "Sports Tuner", "device_id": "SPORTS01", "device_auth": "auth",
		"firmware_version": "20240101", "tuner_count": 2, "port": 47604, "is_enabled": true,
		"channel_group_ids": []interface{}{sportsGroupID},
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)
	var device map[string]interface{}
	decodeResponse(t, rec, &device)
	deviceGroups := device["channel_group_ids"].([]interface{})
	require.Len(t, deviceGroups, 1)
	assert.Equal(t, sportsGroupID, deviceGroups[0])

	t.Run("default lineup shows all enabled channels", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/lineup.json", nil, "")
		require.Equal(t, http.StatusOK, rec.Code)
		var lineup []map[string]interface{}
		decodeResponse(t, rec, &lineup)
		assert.Len(t, lineup, 3)
	})

	t.Run("device has group filter configured", func(t *testing.T) {
		deviceID := device["id"].(string)
		rec := doRequest(t, env, "GET", "/api/hdhr/devices/"+deviceID, nil, env.adminToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var d map[string]interface{}
		decodeResponse(t, rec, &d)
		groups := d["channel_group_ids"].([]interface{})
		require.Len(t, groups, 1)
		assert.Equal(t, sportsGroupID, groups[0])
	})
}

func TestIntegration_HDHR_NoDevice(t *testing.T) {
	env := setupFullEnv(t)

	rec := doRequest(t, env, "GET", "/discover.json", nil, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = doRequest(t, env, "GET", "/device.xml", nil, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func createTestChannel(t *testing.T, env *fullTestEnv, name string, token string) string {
	t.Helper()
	rec := doRequest(t, env, "POST", "/api/channels/", map[string]interface{}{
		"name": name, "tvg_id": name, "is_enabled": true,
	}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	var ch map[string]interface{}
	decodeResponse(t, rec, &ch)
	return ch["id"].(string)
}

func TestIntegration_ScheduleRecording_CRUD(t *testing.T) {
	env := setupFullEnv(t)
	channelID := createTestChannel(t, env, "ITV1", env.adminToken)

	var scheduleID string
	startAt := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	stopAt := startAt.Add(1 * time.Hour)

	t.Run("list empty", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/recordings/schedule", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var list []map[string]interface{}
		decodeResponse(t, rec, &list)
		assert.Len(t, list, 0)
	})

	t.Run("create", func(t *testing.T) {
		rec := doRequest(t, env, "POST", "/api/recordings/schedule", map[string]interface{}{
			"channel_id":    channelID,
			"channel_name":  "ITV1",
			"program_title": "Coronation Street",
			"start_at":      startAt.Format(time.RFC3339),
			"stop_at":       stopAt.Format(time.RFC3339),
		}, env.adminToken)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var sr map[string]interface{}
		decodeResponse(t, rec, &sr)
		assert.Equal(t, "ITV1", sr["channel_name"])
		assert.Equal(t, "Coronation Street", sr["program_title"])
		assert.Equal(t, "pending", sr["status"])
		scheduleID = sr["id"].(string)
	})

	t.Run("list", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/recordings/schedule", nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var list []map[string]interface{}
		decodeResponse(t, rec, &list)
		assert.Len(t, list, 1)
		assert.Equal(t, scheduleID, list[0]["id"])
	})

	t.Run("get", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/recordings/schedule/"+scheduleID, nil, env.adminToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var sr map[string]interface{}
		decodeResponse(t, rec, &sr)
		assert.Equal(t, scheduleID, sr["id"])
		assert.Equal(t, "Coronation Street", sr["program_title"])
	})

	t.Run("delete", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/recordings/schedule/"+scheduleID, nil, env.adminToken)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("verify deleted", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/recordings/schedule/"+scheduleID, nil, env.adminToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIntegration_ScheduleRecording_Duplicate(t *testing.T) {
	env := setupFullEnv(t)
	channelID := createTestChannel(t, env, "BBC One", env.adminToken)

	startAt := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	stopAt := startAt.Add(1 * time.Hour)

	body := map[string]interface{}{
		"channel_id":    channelID,
		"channel_name":  "BBC One",
		"program_title": "EastEnders",
		"start_at":      startAt.Format(time.RFC3339),
		"stop_at":       stopAt.Format(time.RFC3339),
	}

	rec := doRequest(t, env, "POST", "/api/recordings/schedule", body, env.adminToken)
	assert.Equal(t, http.StatusCreated, rec.Code)

	rec = doRequest(t, env, "POST", "/api/recordings/schedule", body, env.adminToken)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestIntegration_ScheduleRecording_InvalidChannel(t *testing.T) {
	env := setupFullEnv(t)

	startAt := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	stopAt := startAt.Add(1 * time.Hour)

	rec := doRequest(t, env, "POST", "/api/recordings/schedule", map[string]interface{}{
		"channel_id":    "nonexistent-id",
		"channel_name":  "Fake",
		"program_title": "Nothing",
		"start_at":      startAt.Format(time.RFC3339),
		"stop_at":       stopAt.Format(time.RFC3339),
	}, env.adminToken)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestIntegration_ScheduleRecording_UserIsolation(t *testing.T) {
	env := setupFullEnv(t)
	channelID := createTestChannel(t, env, "Channel 4", env.adminToken)

	startAt := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	stopAt := startAt.Add(1 * time.Hour)

	rec := doRequest(t, env, "POST", "/api/recordings/schedule", map[string]interface{}{
		"channel_id":    channelID,
		"channel_name":  "Channel 4",
		"program_title": "Hollyoaks",
		"start_at":      startAt.Format(time.RFC3339),
		"stop_at":       stopAt.Format(time.RFC3339),
	}, env.adminToken)
	require.Equal(t, http.StatusCreated, rec.Code)
	var sr map[string]interface{}
	decodeResponse(t, rec, &sr)
	scheduleID := sr["id"].(string)

	t.Run("user cannot see admin schedule", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/recordings/schedule", nil, env.userToken)
		assert.Equal(t, http.StatusOK, rec.Code)
		var list []map[string]interface{}
		decodeResponse(t, rec, &list)
		assert.Len(t, list, 0)
	})

	t.Run("user cannot get admin schedule", func(t *testing.T) {
		rec := doRequest(t, env, "GET", "/api/recordings/schedule/"+scheduleID, nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("user cannot cancel admin schedule", func(t *testing.T) {
		rec := doRequest(t, env, "DELETE", "/api/recordings/schedule/"+scheduleID, nil, env.userToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}
