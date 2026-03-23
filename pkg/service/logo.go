package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/httputil"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

const placeholderLogo = `data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='200' height='200' viewBox='0 0 200 200'%3E%3Crect width='200' height='200' rx='20' fill='%23374151'/%3E%3Ctext x='100' y='115' font-family='sans-serif' font-size='80' fill='%239CA3AF' text-anchor='middle'%3ETV%3C/text%3E%3C/svg%3E`

type LogoService struct {
	repo           *repository.LogoRepository
	config         *config.Config
	httpClient     *http.Client
	logosDir       string
	streamLogosDir string
	log            zerolog.Logger

	streamLogoMu    sync.RWMutex
	streamLogoCache map[string]string
}

func NewLogoService(
	repo *repository.LogoRepository,
	cfg *config.Config,
	log zerolog.Logger,
) *LogoService {
	staticRoot := filepath.Join(filepath.Dir(cfg.DatabasePath), "static")
	timeout := 10 * time.Second
	if cfg.Settings != nil {
		timeout = cfg.Settings.Network.LogoDownloadTimeout
	}
	return &LogoService{
		repo:            repo,
		config:          cfg,
		httpClient:      &http.Client{Timeout: timeout},
		logosDir:        filepath.Join(staticRoot, "logos"),
		streamLogosDir:  filepath.Join(staticRoot, "streams", "logoscache"),
		log:             log.With().Str("service", "logo").Logger(),
		streamLogoCache: make(map[string]string),
	}
}

func (s *LogoService) EnsureDir() {
	os.MkdirAll(s.logosDir, 0755)
	os.MkdirAll(s.streamLogosDir, 0755)
	s.buildStreamLogoCache()
}

func (s *LogoService) buildStreamLogoCache() {
	entries, err := os.ReadDir(s.streamLogosDir)
	if err != nil {
		return
	}
	s.streamLogoMu.Lock()
	defer s.streamLogoMu.Unlock()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		hash := strings.TrimSuffix(name, filepath.Ext(name))
		s.streamLogoCache[hash] = name
	}
	s.log.Info().Int("count", len(s.streamLogoCache)).Msg("loaded stream logo cache")
}

func (s *LogoService) Create(ctx context.Context, logo *models.Logo) error {
	if err := s.repo.Create(ctx, logo); err != nil {
		return err
	}
	go s.downloadLogo(context.Background(), logo.ID, logo.URL)
	return nil
}

func (s *LogoService) Update(ctx context.Context, logo *models.Logo) error {
	old, err := s.repo.GetByID(ctx, logo.ID)
	if err != nil {
		return err
	}
	urlChanged := old.URL != logo.URL
	if err := s.repo.Update(ctx, logo); err != nil {
		return err
	}
	if urlChanged {
		if old.CachedFilename != "" {
			os.Remove(filepath.Join(s.logosDir, old.CachedFilename))
			s.repo.UpdateCachedFilename(ctx, logo.ID, "")
		}
		go s.downloadLogo(context.Background(), logo.ID, logo.URL)
	}
	return nil
}

func (s *LogoService) Delete(ctx context.Context, id string) error {
	logo, err := s.repo.GetByID(ctx, id)
	if err == nil && logo.CachedFilename != "" {
		os.Remove(filepath.Join(s.logosDir, logo.CachedFilename))
	}
	return s.repo.Delete(ctx, id)
}

func (s *LogoService) List(ctx context.Context) ([]models.Logo, error) {
	return s.repo.List(ctx)
}

func (s *LogoService) GetByID(ctx context.Context, id string) (*models.Logo, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *LogoService) GetByURL(ctx context.Context, url string) (*models.Logo, error) {
	return s.repo.GetByURL(ctx, url)
}

func (s *LogoService) CacheAll(ctx context.Context) {
	logos, err := s.repo.List(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to list logos for caching")
		return
	}
	for _, logo := range logos {
		if logo.CachedFilename != "" || logo.URL == "" {
			continue
		}
		s.downloadLogo(ctx, logo.ID, logo.URL)
	}
}

func (s *LogoService) downloadLogo(ctx context.Context, id, url string) {
	if url == "" {
		return
	}

	resp, err := httputil.Fetch(ctx, s.httpClient, s.config, url)
	if err != nil {
		s.log.Debug().Err(err).Str("url", url).Msg("failed to download logo")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.log.Debug().Int("status", resp.StatusCode).Str("url", url).Msg("logo download non-200")
		return
	}

	ext := detectExtension(resp.Header.Get("Content-Type"), url)
	filename := id + ext

	f, err := os.Create(filepath.Join(s.logosDir, filename))
	if err != nil {
		s.log.Error().Err(err).Msg("failed to create logo file")
		return
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(filepath.Join(s.logosDir, filename))
		s.log.Error().Err(err).Msg("failed to write logo file")
		return
	}
	f.Close()

	if err := s.repo.UpdateCachedFilename(context.Background(), id, filename); err != nil {
		s.log.Error().Err(err).Msg("failed to update cached filename in db")
	}
}

func (s *LogoService) BaseURL() string {
	return fmt.Sprintf("%s:%d", s.config.BaseURL, s.config.Port)
}

func isDisplayableURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") ||
		strings.HasPrefix(u, "/") || strings.HasPrefix(u, "data:")
}

func (s *LogoService) Resolve(url string) string {
	if url == "" {
		return placeholderLogo
	}
	baseURL := s.BaseURL()
	if cached := s.StreamLogoFilename(url); cached != "" {
		return baseURL + "/static/" + cached
	}
	if isDisplayableURL(url) {
		return url
	}
	return placeholderLogo
}

func (s *LogoService) ResolveChannel(ch models.Channel) string {
	if ch.LogoCached != "" {
		return s.BaseURL() + "/static/logos/" + ch.LogoCached
	}
	if ch.Logo != "" && isDisplayableURL(ch.Logo) {
		return ch.Logo
	}
	return placeholderLogo
}

func (s *LogoService) ResolveChannelLogos(channels []models.Channel) {
	for i := range channels {
		channels[i].Logo = s.ResolveChannel(channels[i])
	}
}

func (s *LogoService) CacheStreamLogos(ctx context.Context, streams []models.Stream) {
	for _, stream := range streams {
		if stream.Logo == "" {
			continue
		}
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(stream.Logo)))[:16]
		s.streamLogoMu.RLock()
		_, exists := s.streamLogoCache[hash]
		s.streamLogoMu.RUnlock()
		if exists {
			continue
		}
		s.downloadStreamLogo(ctx, stream.Logo, hash)
	}
}

func (s *LogoService) StreamLogoFilename(url string) string {
	if url == "" {
		return ""
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(url)))[:16]
	s.streamLogoMu.RLock()
	filename, ok := s.streamLogoCache[hash]
	s.streamLogoMu.RUnlock()
	if ok {
		return "streams/logoscache/" + filename
	}
	return ""
}

func (s *LogoService) downloadStreamLogo(ctx context.Context, url, hash string) {
	resp, err := httputil.Fetch(ctx, s.httpClient, s.config, url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	ext := detectExtension(resp.Header.Get("Content-Type"), url)
	filename := hash + ext

	f, err := os.Create(filepath.Join(s.streamLogosDir, filename))
	if err != nil {
		return
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(filepath.Join(s.streamLogosDir, filename))
		return
	}
	f.Close()

	s.streamLogoMu.Lock()
	s.streamLogoCache[hash] = filename
	s.streamLogoMu.Unlock()
}

func detectExtension(contentType, url string) string {
	if contentType != "" {
		ct := strings.SplitN(contentType, ";", 2)[0]
		ct = strings.TrimSpace(ct)
		exts, _ := mime.ExtensionsByType(ct)
		if len(exts) > 0 {
			for _, e := range exts {
				if e == ".png" || e == ".jpg" || e == ".jpeg" || e == ".svg" || e == ".webp" || e == ".gif" {
					return e
				}
			}
			return exts[0]
		}
	}
	ext := filepath.Ext(strings.SplitN(url, "?", 2)[0])
	if ext != "" && len(ext) <= 5 {
		return ext
	}
	return ".png"
}
