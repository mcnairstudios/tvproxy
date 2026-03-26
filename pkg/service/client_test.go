package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestMatchRule(t *testing.T) {
	tests := []struct {
		name   string
		header map[string]string
		rule   models.ClientMatchRule
		want   bool
	}{
		{
			name:   "exists match — header present",
			header: map[string]string{"User-Agent": "VLC/3.0"},
			rule:   models.ClientMatchRule{HeaderName: "User-Agent", MatchType: "exists"},
			want:   true,
		},
		{
			name:   "exists match — header absent",
			header: map[string]string{},
			rule:   models.ClientMatchRule{HeaderName: "User-Agent", MatchType: "exists"},
			want:   false,
		},
		{
			name:   "contains match — substring present",
			header: map[string]string{"User-Agent": "Lavf/60.3.100"},
			rule:   models.ClientMatchRule{HeaderName: "User-Agent", MatchType: "contains", MatchValue: "Lavf/"},
			want:   true,
		},
		{
			name:   "contains match — substring absent",
			header: map[string]string{"User-Agent": "Mozilla/5.0"},
			rule:   models.ClientMatchRule{HeaderName: "User-Agent", MatchType: "contains", MatchValue: "Lavf/"},
			want:   false,
		},
		{
			name:   "contains match — empty header",
			header: map[string]string{},
			rule:   models.ClientMatchRule{HeaderName: "User-Agent", MatchType: "contains", MatchValue: "Lavf/"},
			want:   false,
		},
		{
			name:   "equals match — exact",
			header: map[string]string{"X-Client": "plex"},
			rule:   models.ClientMatchRule{HeaderName: "X-Client", MatchType: "equals", MatchValue: "plex"},
			want:   true,
		},
		{
			name:   "equals match — different",
			header: map[string]string{"X-Client": "vlc"},
			rule:   models.ClientMatchRule{HeaderName: "X-Client", MatchType: "equals", MatchValue: "plex"},
			want:   false,
		},
		{
			name:   "prefix match — starts with",
			header: map[string]string{"User-Agent": "OculusBrowser/25.0"},
			rule:   models.ClientMatchRule{HeaderName: "User-Agent", MatchType: "prefix", MatchValue: "OculusBrowser/"},
			want:   true,
		},
		{
			name:   "prefix match — does not start with",
			header: map[string]string{"User-Agent": "Mozilla/5.0 OculusBrowser/25.0"},
			rule:   models.ClientMatchRule{HeaderName: "User-Agent", MatchType: "prefix", MatchValue: "OculusBrowser/"},
			want:   false,
		},
		{
			name:   "unknown match type — returns false",
			header: map[string]string{"User-Agent": "anything"},
			rule:   models.ClientMatchRule{HeaderName: "User-Agent", MatchType: "regex", MatchValue: ".*"},
			want:   false,
		},
		{
			name:   "case-insensitive header lookup",
			header: map[string]string{"user-agent": "VLC/3.0"},
			rule:   models.ClientMatchRule{HeaderName: "User-Agent", MatchType: "contains", MatchValue: "VLC/"},
			want:   true,
		},
		{
			name:   "exists match — header present with empty value",
			header: map[string]string{"Icy-Metadata": ""},
			rule:   models.ClientMatchRule{HeaderName: "Icy-Metadata", MatchType: "exists"},
			want:   false, // Go's Header.Get returns "" for both absent and empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			for k, v := range tt.header {
				req.Header.Set(k, v)
			}
			got := matchRule(req, tt.rule)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesAllRules(t *testing.T) {
	svc := NewClientService(nil, nil, NewSettingsService(nil, nil, zerolog.Nop()), zerolog.Nop())

	tests := []struct {
		name    string
		headers map[string]string
		rules   []models.ClientMatchRule
		want    bool
	}{
		{
			name:    "all rules match — AND logic",
			headers: map[string]string{"User-Agent": "Lavf/60.3.100", "Icy-Metadata": "1"},
			rules: []models.ClientMatchRule{
				{HeaderName: "User-Agent", MatchType: "contains", MatchValue: "Lavf/"},
				{HeaderName: "Icy-Metadata", MatchType: "exists"},
			},
			want: true,
		},
		{
			name:    "one rule fails — AND logic",
			headers: map[string]string{"User-Agent": "Lavf/60.3.100"},
			rules: []models.ClientMatchRule{
				{HeaderName: "User-Agent", MatchType: "contains", MatchValue: "Lavf/"},
				{HeaderName: "Icy-Metadata", MatchType: "exists"},
			},
			want: false,
		},
		{
			name:    "empty rules — vacuously true",
			headers: map[string]string{},
			rules:   []models.ClientMatchRule{},
			want:    true,
		},
		{
			name:    "single rule match",
			headers: map[string]string{"User-Agent": "VLC/3.0.20"},
			rules: []models.ClientMatchRule{
				{HeaderName: "User-Agent", MatchType: "contains", MatchValue: "VLC/"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			client := models.Client{Name: "test", MatchRules: tt.rules}
			got := svc.matchesAllRules(req, client, false)
			assert.Equal(t, tt.want, got)
		})
	}
}
