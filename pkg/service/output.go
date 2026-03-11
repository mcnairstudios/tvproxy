package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// OutputService generates M3U playlists and XMLTV EPG data from the configured channels.
type OutputService struct {
	channelRepo        *repository.ChannelRepository
	channelGroupRepo   *repository.ChannelGroupRepository
	streamRepo         *repository.StreamRepository
	channelProfileRepo *repository.ChannelProfileRepository
	streamProfileRepo  *repository.StreamProfileRepository
	epgDataRepo        *repository.EPGDataRepository
	programDataRepo    *repository.ProgramDataRepository
	config             *config.Config
	log                zerolog.Logger
}

// NewOutputService creates a new OutputService.
func NewOutputService(
	channelRepo *repository.ChannelRepository,
	channelGroupRepo *repository.ChannelGroupRepository,
	streamRepo *repository.StreamRepository,
	channelProfileRepo *repository.ChannelProfileRepository,
	streamProfileRepo *repository.StreamProfileRepository,
	epgDataRepo *repository.EPGDataRepository,
	programDataRepo *repository.ProgramDataRepository,
	cfg *config.Config,
	log zerolog.Logger,
) *OutputService {
	return &OutputService{
		channelRepo:        channelRepo,
		channelGroupRepo:   channelGroupRepo,
		streamRepo:         streamRepo,
		channelProfileRepo: channelProfileRepo,
		streamProfileRepo:  streamProfileRepo,
		epgDataRepo:        epgDataRepo,
		programDataRepo:    programDataRepo,
		config:             cfg,
		log:                log.With().Str("service", "output").Logger(),
	}
}

// resolveSourceURL returns the URL of the first active stream for the channel.
func (s *OutputService) resolveSourceURL(ctx context.Context, channelID int64) string {
	streams, err := s.channelRepo.GetStreams(ctx, channelID)
	if err != nil || len(streams) == 0 {
		return ""
	}
	for _, cs := range streams {
		stream, err := s.streamRepo.GetByID(ctx, cs.StreamID)
		if err != nil || !stream.IsActive {
			continue
		}
		return stream.URL
	}
	return ""
}

// placeholderLogo is a minimal SVG data URI used when a channel has no logo assigned.
const placeholderLogo = `data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='200' height='200' viewBox='0 0 200 200'%3E%3Crect width='200' height='200' rx='20' fill='%23374151'/%3E%3Ctext x='100' y='115' font-family='sans-serif' font-size='80' fill='%239CA3AF' text-anchor='middle'%3ETV%3C/text%3E%3C/svg%3E`

// channelEPGID returns the EPG channel ID for a channel. If the channel has a
// tvg_id assigned it is used directly; otherwise a synthetic ID is generated
// from the channel's database ID so the M3U tvg-id and XMLTV channel id match.
func channelEPGID(ch models.Channel) string {
	if ch.TvgID != "" {
		return ch.TvgID
	}
	return fmt.Sprintf("tvproxy.%d", ch.ID)
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

	baseURL := fmt.Sprintf("%s:%d", s.config.BaseURL, s.config.Port)

	var b strings.Builder
	b.WriteString("#EXTM3U\n")

	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}

		// Build the EXTINF line
		b.WriteString("#EXTINF:-1")

		// Always emit tvg-id so Plex can link to the EPG
		b.WriteString(fmt.Sprintf(" tvg-id=\"%s\"", channelEPGID(ch)))

		// Add tvg-chno for channel number
		b.WriteString(fmt.Sprintf(" tvg-chno=\"%d\"", ch.ChannelNumber))

		// Add tvg-name
		b.WriteString(fmt.Sprintf(" tvg-name=\"%s\"", ch.Name))

		// Always emit tvg-logo; use placeholder when none assigned
		logo := ch.Logo
		if logo == "" {
			logo = placeholderLogo
		}
		b.WriteString(fmt.Sprintf(" tvg-logo=\"%s\"", logo))

		// Add group-title if channel belongs to a group
		if ch.ChannelGroupID != nil {
			if name, ok := groupNames[*ch.ChannelGroupID]; ok {
				b.WriteString(fmt.Sprintf(" group-title=\"%s\"", name))
			}
		}

		b.WriteString(fmt.Sprintf(",%s\n", ch.Name))

		// Stream URL — direct source for direct mode, proxy for everything else
		streamURL := fmt.Sprintf("%s/channel/%d", baseURL, ch.ID)
		mode, _ := ResolveStreamMode(ctx, &ch, s.channelProfileRepo, s.streamProfileRepo, s.log)
		if mode == "direct" {
			if src := s.resolveSourceURL(ctx, ch.ID); src != "" {
				streamURL = src
			}
		}
		b.WriteString(streamURL + "\n")
	}

	return b.String(), nil
}

// GenerateEPG generates XMLTV-format EPG data from all stored EPG data and programs.
// Channels that have a tvg_id get their real EPG data. Channels without a tvg_id
// get a synthetic channel entry so that Plex/Jellyfin can still link them.
func (s *OutputService) GenerateEPG(ctx context.Context) (string, error) {
	channels, err := s.channelRepo.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channels: %w", err)
	}

	// Build a set of enabled channel tvg_ids for filtering EPG data,
	// and collect channels without EPG for synthetic entries.
	enabledTvgIDs := make(map[string]bool, len(channels))
	var noEPGChannels []models.Channel
	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}
		if ch.TvgID != "" {
			enabledTvgIDs[ch.TvgID] = true
		} else {
			noEPGChannels = append(noEPGChannels, ch)
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

	// Write channel elements for real EPG entries
	for _, epg := range epgDataList {
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

	// Write synthetic channel entries for channels without EPG
	for _, ch := range noEPGChannels {
		syntheticID := channelEPGID(ch)
		b.WriteString(fmt.Sprintf(`  <channel id="%s">`, xmlEscape(syntheticID)))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf(`    <display-name>%s</display-name>`, xmlEscape(ch.Name)))
		b.WriteString("\n")
		logo := ch.Logo
		if logo == "" {
			logo = placeholderLogo
		}
		b.WriteString(fmt.Sprintf(`    <icon src="%s" />`, xmlEscape(logo)))
		b.WriteString("\n")
		b.WriteString("  </channel>\n")
	}

	// Write programme elements for real EPG entries
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

// GenerateM3UForGroups generates an M3U playlist filtered to channels in the given groups.
// If groupIDs is empty, all enabled channels are included (same as GenerateM3U).
func (s *OutputService) GenerateM3UForGroups(ctx context.Context, groupIDs []int64, baseURL string) (string, error) {
	if len(groupIDs) == 0 {
		return s.GenerateM3U(ctx)
	}

	groupSet := make(map[int64]bool, len(groupIDs))
	for _, gid := range groupIDs {
		groupSet[gid] = true
	}

	channels, err := s.channelRepo.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channels: %w", err)
	}

	groups, err := s.channelGroupRepo.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channel groups: %w", err)
	}
	groupNames := make(map[int64]string, len(groups))
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}

	var b strings.Builder
	b.WriteString("#EXTM3U\n")

	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}
		if ch.ChannelGroupID == nil || !groupSet[*ch.ChannelGroupID] {
			continue
		}

		b.WriteString("#EXTINF:-1")
		b.WriteString(fmt.Sprintf(" tvg-id=\"%s\"", channelEPGID(ch)))
		b.WriteString(fmt.Sprintf(" tvg-chno=\"%d\"", ch.ChannelNumber))
		b.WriteString(fmt.Sprintf(" tvg-name=\"%s\"", ch.Name))

		logo := ch.Logo
		if logo == "" {
			logo = placeholderLogo
		}
		b.WriteString(fmt.Sprintf(" tvg-logo=\"%s\"", logo))

		if name, ok := groupNames[*ch.ChannelGroupID]; ok {
			b.WriteString(fmt.Sprintf(" group-title=\"%s\"", name))
		}

		b.WriteString(fmt.Sprintf(",%s\n", ch.Name))

		streamURL := fmt.Sprintf("%s/channel/%d", baseURL, ch.ID)
		mode, _ := ResolveStreamMode(ctx, &ch, s.channelProfileRepo, s.streamProfileRepo, s.log)
		if mode == "direct" {
			if src := s.resolveSourceURL(ctx, ch.ID); src != "" {
				streamURL = src
			}
		}
		b.WriteString(streamURL + "\n")
	}

	return b.String(), nil
}

// GenerateEPGForGroups generates XMLTV EPG data filtered to channels in the given groups.
// If groupIDs is empty, all data is included (same as GenerateEPG).
func (s *OutputService) GenerateEPGForGroups(ctx context.Context, groupIDs []int64) (string, error) {
	if len(groupIDs) == 0 {
		return s.GenerateEPG(ctx)
	}

	groupSet := make(map[int64]bool, len(groupIDs))
	for _, gid := range groupIDs {
		groupSet[gid] = true
	}

	channels, err := s.channelRepo.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channels: %w", err)
	}

	enabledTvgIDs := make(map[string]bool, len(channels))
	var noEPGChannels []models.Channel
	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}
		if ch.ChannelGroupID == nil || !groupSet[*ch.ChannelGroupID] {
			continue
		}
		if ch.TvgID != "" {
			enabledTvgIDs[ch.TvgID] = true
		} else {
			noEPGChannels = append(noEPGChannels, ch)
		}
	}

	epgDataList, err := s.epgDataRepo.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing epg data: %w", err)
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE tv SYSTEM "xmltv.dtd">` + "\n")
	b.WriteString(`<tv generator-info-name="tvproxy">` + "\n")

	for _, epg := range epgDataList {
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

	for _, ch := range noEPGChannels {
		syntheticID := channelEPGID(ch)
		b.WriteString(fmt.Sprintf(`  <channel id="%s">`, xmlEscape(syntheticID)))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf(`    <display-name>%s</display-name>`, xmlEscape(ch.Name)))
		b.WriteString("\n")
		logo := ch.Logo
		if logo == "" {
			logo = placeholderLogo
		}
		b.WriteString(fmt.Sprintf(`    <icon src="%s" />`, xmlEscape(logo)))
		b.WriteString("\n")
		b.WriteString("  </channel>\n")
	}

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

