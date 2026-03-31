package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/defaults"
)

type Config struct {
	Host string
	Port int

	DatabasePath string

	JWTSecret          string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration

	APIKey string

	LogLevel string
	LogJSON  bool

	BaseURL string

	UserAgent    string
	BypassHeader string
	BypassSecret string

	M3URefreshInterval time.Duration
	EPGRefreshInterval time.Duration

	VODOutputDir      string
	VODSessionTimeout time.Duration

	RecordDir             string
	RecordDefaultDuration time.Duration
	RecordStopBuffer      time.Duration

	Settings *defaults.Settings
}

func Load() *Config {
	return &Config{
		Host:                  envStr("TVPROXY_HOST", "0.0.0.0"),
		Port:                  envInt("TVPROXY_PORT", 8080),
		DatabasePath:          envStr("TVPROXY_DB_PATH", "tvproxy.db"),
		JWTSecret:             envStr("TVPROXY_JWT_SECRET", "change-me-in-production"),
		AccessTokenExpiry:     envDuration("TVPROXY_ACCESS_TOKEN_EXPIRY", 15*time.Minute),
		RefreshTokenExpiry:    envDuration("TVPROXY_REFRESH_TOKEN_EXPIRY", 7*24*time.Hour),
		APIKey:                envStr("TVPROXY_API_KEY", ""),
		BaseURL:               envStr("TVPROXY_BASE_URL", ""),
		UserAgent:             envStr("TVPROXY_USER_AGENT", "TVProxy"),
		BypassHeader:          envStr("TVPROXY_BYPASS_HEADER", ""),
		BypassSecret:          envStr("TVPROXY_BYPASS_SECRET", ""),
		LogLevel:              envStr("TVPROXY_LOG_LEVEL", "info"),
		LogJSON:               envBool("TVPROXY_LOG_JSON", false),
		M3URefreshInterval:    envDuration("TVPROXY_M3U_REFRESH_INTERVAL", 24*time.Hour),
		EPGRefreshInterval:    envDuration("TVPROXY_EPG_REFRESH_INTERVAL", 12*time.Hour),
		VODOutputDir:          envStr("TVPROXY_VOD_OUTPUT_DIR", "/record"),
		VODSessionTimeout:     envDuration("TVPROXY_VOD_SESSION_TIMEOUT", 5*time.Minute),
		RecordDir:             envStr("TVPROXY_RECORD_DIR", "/record"),
		RecordDefaultDuration: envDuration("TVPROXY_RECORD_DEFAULT_DURATION", 4*time.Hour),
		RecordStopBuffer:      envDuration("TVPROXY_RECORD_STOP_BUFFER", 5*time.Minute),
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
			if d < 0 {
				log.Printf("WARNING: %s has negative duration %s, using default %s", key, d, fallback)
				return fallback
			}
			return d
		}
	}
	return fallback
}
