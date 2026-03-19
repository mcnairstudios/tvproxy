package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Server
	Host string
	Port int

	// Database
	DatabasePath string

	// JWT
	JWTSecret          string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration

	// API Key
	APIKey string

	// Logging
	LogLevel string
	LogJSON  bool

	// Base URL (for SSDP discovery and HDHR links)
	BaseURL string

	// Upstream HTTP
	UserAgent string

	// Workers
	M3URefreshInterval time.Duration
	EPGRefreshInterval time.Duration

	// VOD
	VODTempDir        string
	VODSessionTimeout time.Duration

	// Recording
	RecordDir             string
	RecordDefaultDuration time.Duration
}

func Load() *Config {
	return &Config{
		Host:               envStr("TVPROXY_HOST", "0.0.0.0"),
		Port:               envInt("TVPROXY_PORT", 8080),
		DatabasePath:       envStr("TVPROXY_DB_PATH", "tvproxy.db"),
		JWTSecret:          envStr("TVPROXY_JWT_SECRET", "change-me-in-production"),
		AccessTokenExpiry:  envDuration("TVPROXY_ACCESS_TOKEN_EXPIRY", 15*time.Minute),
		RefreshTokenExpiry: envDuration("TVPROXY_REFRESH_TOKEN_EXPIRY", 7*24*time.Hour),
		APIKey:             envStr("TVPROXY_API_KEY", ""),
		BaseURL:            envStr("TVPROXY_BASE_URL", ""),
		UserAgent:          envStr("TVPROXY_USER_AGENT", "TVProxy"),
		LogLevel:           envStr("TVPROXY_LOG_LEVEL", "info"),
		LogJSON:            envBool("TVPROXY_LOG_JSON", false),
		M3URefreshInterval: envDuration("TVPROXY_M3U_REFRESH_INTERVAL", 24*time.Hour),
		EPGRefreshInterval: envDuration("TVPROXY_EPG_REFRESH_INTERVAL", 12*time.Hour),
		VODTempDir:            envStr("TVPROXY_VOD_TEMP_DIR", "/tmp/tvproxy-vod"),
		VODSessionTimeout:    envDuration("TVPROXY_VOD_SESSION_TIMEOUT", 5*time.Minute),
		RecordDir:             envStr("TVPROXY_RECORD_DIR", "/record"),
		RecordDefaultDuration: envDuration("TVPROXY_RECORD_DEFAULT_DURATION", 4*time.Hour),
	}
}

func (c *Config) ListenAddr() string {
	return c.Host + ":" + strconv.Itoa(c.Port)
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
