package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// OutputService generates M3U playlists and XMLTV EPG data from the configured channels.
type OutputService struct {
	channelRepo      *repository.ChannelRepository
	channelGroupRepo *repository.ChannelGroupRepository
	epgDataRepo      *repository.EPGDataRepository
	programDataRepo  *repository.ProgramDataRepository
	config           *config.Config
	log              zerolog.Logger
}

// NewOutputService creates a new OutputService.
func NewOutputService(
	channelRepo *repository.ChannelRepository,
	channelGroupRepo *repository.ChannelGroupRepository,
	epgDataRepo *repository.EPGDataRepository,
	programDataRepo *repository.ProgramDataRepository,
	cfg *config.Config,
	log zerolog.Logger,
) *OutputService {
	return &OutputService{
		channelRepo:      channelRepo,
		channelGroupRepo: channelGroupRepo,
		epgDataRepo:      epgDataRepo,
		programDataRepo:  programDataRepo,
		config:           cfg,
		log:              log.With().Str("service", "output").Logger(),
	}
}

// GenerateM3U generates an M3U playlist from all enabled channels.
func (s *OutputService) GenerateM3U(ctx context.Context) (string, error) {
	channels, err := s.channelRepo.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channels: %w", err)
	}

	// Build a map of channel group names
	groups, err := s.channelGroupRepo.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channel groups: %w", err)
	}
	groupNames := make(map[int64]string, len(groups))
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}

	baseURL := fmt.Sprintf("http://%s:%d", s.config.Host, s.config.Port)

	var b strings.Builder
	b.WriteString("#EXTM3U\n")

	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}

		// Build the EXTINF line
		b.WriteString("#EXTINF:-1")

		// Add tvg-id if present
		if ch.TvgID != "" {
			b.WriteString(fmt.Sprintf(" tvg-id=\"%s\"", ch.TvgID))
		}

		// Add tvg-chno for channel number
		b.WriteString(fmt.Sprintf(" tvg-chno=\"%d\"", ch.ChannelNumber))

		// Add tvg-name
		b.WriteString(fmt.Sprintf(" tvg-name=\"%s\"", ch.Name))

		// Add tvg-logo if present
		if ch.Logo != "" {
			b.WriteString(fmt.Sprintf(" tvg-logo=\"%s\"", ch.Logo))
		}

		// Add group-title if channel belongs to a group
		if ch.ChannelGroupID != nil {
			if name, ok := groupNames[*ch.ChannelGroupID]; ok {
				b.WriteString(fmt.Sprintf(" group-title=\"%s\"", name))
			}
		}

		b.WriteString(fmt.Sprintf(",%s\n", ch.Name))

		// Stream URL
		b.WriteString(fmt.Sprintf("%s/api/stream/%d\n", baseURL, ch.ID))
	}

	return b.String(), nil
}

// GenerateEPG generates XMLTV-format EPG data from all stored EPG data and programs.
func (s *OutputService) GenerateEPG(ctx context.Context) (string, error) {
	// Get all channels to map tvg_id to channel info
	channels, err := s.channelRepo.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channels: %w", err)
	}

	// Build a set of enabled channel tvg_ids for filtering
	enabledTvgIDs := make(map[string]bool, len(channels))
	for _, ch := range channels {
		if ch.IsEnabled && ch.TvgID != "" {
			enabledTvgIDs[ch.TvgID] = true
		}
	}

	// Get all EPG data
	epgDataList, err := s.epgDataRepo.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing epg data: %w", err)
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE tv SYSTEM "xmltv.dtd">` + "\n")
	b.WriteString(`<tv generator-info-name="tvproxy">` + "\n")

	// Write channel elements
	for _, epg := range epgDataList {
		// Only include channels that match enabled channels
		if !enabledTvgIDs[epg.ChannelID] {
			continue
		}

		b.WriteString(fmt.Sprintf(`  <channel id="%s">`, xmlEscape(epg.ChannelID)))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf(`    <display-name>%s</display-name>`, xmlEscape(epg.Name)))
		b.WriteString("\n")
		if epg.Icon != "" {
			b.WriteString(fmt.Sprintf(`    <icon src="%s" />`, xmlEscape(epg.Icon)))
			b.WriteString("\n")
		}
		b.WriteString("  </channel>\n")
	}

	// Write programme elements
	for _, epg := range epgDataList {
		if !enabledTvgIDs[epg.ChannelID] {
			continue
		}

		programs, err := s.programDataRepo.ListByEPGDataID(ctx, epg.ID)
		if err != nil {
			s.log.Error().Err(err).Int64("epg_data_id", epg.ID).Msg("failed to list programs")
			continue
		}

		for _, prog := range programs {
			start := prog.Start.Format("20060102150405 -0700")
			stop := prog.Stop.Format("20060102150405 -0700")

			b.WriteString(fmt.Sprintf(`  <programme start="%s" stop="%s" channel="%s">`,
				start, stop, xmlEscape(epg.ChannelID)))
			b.WriteString("\n")
			b.WriteString(fmt.Sprintf(`    <title>%s</title>`, xmlEscape(prog.Title)))
			b.WriteString("\n")

			if prog.Description != "" {
				b.WriteString(fmt.Sprintf(`    <desc>%s</desc>`, xmlEscape(prog.Description)))
				b.WriteString("\n")
			}
			if prog.Category != "" {
				b.WriteString(fmt.Sprintf(`    <category>%s</category>`, xmlEscape(prog.Category)))
				b.WriteString("\n")
			}
			if prog.EpisodeNum != "" {
				b.WriteString(fmt.Sprintf(`    <episode-num system="onscreen">%s</episode-num>`, xmlEscape(prog.EpisodeNum)))
				b.WriteString("\n")
			}
			if prog.Icon != "" {
				b.WriteString(fmt.Sprintf(`    <icon src="%s" />`, xmlEscape(prog.Icon)))
				b.WriteString("\n")
			}

			b.WriteString("  </programme>\n")
		}
	}

	b.WriteString("</tv>\n")

	return b.String(), nil
}

// xmlEscape escapes special characters for XML output.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

