package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/xmlutil"
)

type OutputService struct {
	channelStore      store.ChannelStore
	channelGroupStore store.ChannelGroupStore
	epgStore          store.EPGReader
	logoService       *LogoService
	config            *config.Config
	log               zerolog.Logger
}

func NewOutputService(
	channelStore store.ChannelStore,
	channelGroupStore store.ChannelGroupStore,
	epgStore store.EPGReader,
	logoService *LogoService,
	cfg *config.Config,
	log zerolog.Logger,
) *OutputService {
	return &OutputService{
		channelStore:      channelStore,
		channelGroupStore: channelGroupStore,
		epgStore:         epgStore,
		logoService:      logoService,
		config:           cfg,
		log:              log.With().Str("service", "output").Logger(),
	}
}

func channelEPGID(ch models.Channel) string {
	if ch.TvgID != "" {
		return ch.TvgID
	}
	return fmt.Sprintf("tvproxy.%s", ch.ID)
}

func (s *OutputService) baseURL() string {
	return fmt.Sprintf("%s:%d", s.config.BaseURL, s.config.Port)
}

func (s *OutputService) GenerateM3U(ctx context.Context) (string, error) {
	return s.generateM3U(ctx, nil, s.baseURL(), "")
}

func (s *OutputService) GenerateM3UWithExtension(ctx context.Context, ext string) (string, error) {
	return s.generateM3U(ctx, nil, s.baseURL(), ext)
}

func (s *OutputService) GenerateM3UForGroups(ctx context.Context, groupIDs []string, baseURL string) (string, error) {
	if len(groupIDs) == 0 {
		return s.GenerateM3U(ctx)
	}
	return s.generateM3U(ctx, xmlutil.ToStringSet(groupIDs), baseURL, "")
}

func (s *OutputService) GenerateEPG(ctx context.Context) (string, error) {
	return s.generateEPG(ctx, nil)
}

func (s *OutputService) GenerateEPGForGroups(ctx context.Context, groupIDs []string) (string, error) {
	if len(groupIDs) == 0 {
		return s.GenerateEPG(ctx)
	}
	return s.generateEPG(ctx, xmlutil.ToStringSet(groupIDs))
}

func (s *OutputService) generateM3U(ctx context.Context, groupFilter map[string]bool, baseURL string, urlSuffix string) (string, error) {
	channels, err := s.channelStore.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channels: %w", err)
	}

	groups, err := s.channelGroupStore.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channel groups: %w", err)
	}
	groupNames := make(map[string]string, len(groups))
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}

	var b strings.Builder
	b.WriteString("#EXTM3U\n")

	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}
		if groupFilter != nil {
			if ch.ChannelGroupID == nil || !groupFilter[*ch.ChannelGroupID] {
				continue
			}
		}

		b.WriteString("#EXTINF:-1")

		b.WriteString(fmt.Sprintf(" tvg-id=\"%s\"", channelEPGID(ch)))
		b.WriteString(fmt.Sprintf(" tvg-name=\"%s\"", ch.Name))

		logo := s.logoService.ResolveChannel(ch)
		b.WriteString(fmt.Sprintf(" tvg-logo=\"%s\"", logo))

		if ch.ChannelGroupID != nil {
			if name, ok := groupNames[*ch.ChannelGroupID]; ok {
				b.WriteString(fmt.Sprintf(" group-title=\"%s\"", name))
			}
		}

		b.WriteString(fmt.Sprintf(",%s\n", ch.Name))

		streamURL := ResolveChannelURL(ch.ID, baseURL)
		b.WriteString(streamURL + urlSuffix + "\n")
	}

	return b.String(), nil
}

func (s *OutputService) generateEPG(ctx context.Context, groupFilter map[string]bool) (string, error) {
	channels, err := s.channelStore.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channels: %w", err)
	}

	enabledTvgIDs := make(map[string]bool, len(channels))
	var noEPGChannels []models.Channel
	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}
		if groupFilter != nil {
			if ch.ChannelGroupID == nil || !groupFilter[*ch.ChannelGroupID] {
				continue
			}
		}
		if ch.TvgID != "" {
			enabledTvgIDs[ch.TvgID] = true
		} else {
			noEPGChannels = append(noEPGChannels, ch)
		}
	}

	epgDataList, err := s.epgStore.ListEPGData(ctx)
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
		s.writeXMLChannel(&b, epg.ChannelID, epg.Name, epg.Icon)
	}

	for _, ch := range noEPGChannels {
		chLogo := s.logoService.ResolveChannel(ch)
		s.writeXMLChannel(&b, channelEPGID(ch), ch.Name, chLogo)
	}

	for _, epg := range epgDataList {
		if !enabledTvgIDs[epg.ChannelID] {
			continue
		}
		programs, err := s.epgStore.ListPrograms(ctx, epg.ID)
		if err != nil {
			s.log.Error().Err(err).Str("epg_data_id", epg.ID).Msg("failed to list programs")
			continue
		}
		for _, prog := range programs {
			s.writeXMLProgramme(&b, epg.ChannelID, prog)
		}
	}

	b.WriteString("</tv>\n")
	return b.String(), nil
}

func (s *OutputService) writeXMLChannel(b *strings.Builder, id, name, icon string) {
	b.WriteString(fmt.Sprintf(`  <channel id="%s">`, xmlutil.XmlEscape(id)))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(`    <display-name>%s</display-name>`, xmlutil.XmlEscape(name)))
	b.WriteString("\n")
	if icon != "" {
		b.WriteString(fmt.Sprintf(`    <icon src="%s" />`, xmlutil.XmlEscape(icon)))
		b.WriteString("\n")
	}
	b.WriteString("  </channel>\n")
}

func (s *OutputService) writeXMLProgramme(b *strings.Builder, channelID string, prog models.ProgramData) {
	start := prog.Start.Format("20060102150405 -0700")
	stop := prog.Stop.Format("20060102150405 -0700")

	b.WriteString(fmt.Sprintf(`  <programme start="%s" stop="%s" channel="%s">`,
		start, stop, xmlutil.XmlEscape(channelID)))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(`    <title>%s</title>`, xmlutil.XmlEscape(prog.Title)))
	b.WriteString("\n")

	if prog.Subtitle != "" {
		b.WriteString(fmt.Sprintf(`    <sub-title>%s</sub-title>`, xmlutil.XmlEscape(prog.Subtitle)))
		b.WriteString("\n")
	}
	if prog.Description != "" {
		b.WriteString(fmt.Sprintf(`    <desc>%s</desc>`, xmlutil.XmlEscape(prog.Description)))
		b.WriteString("\n")
	}
	if prog.Date != "" {
		b.WriteString(fmt.Sprintf(`    <date>%s</date>`, xmlutil.XmlEscape(prog.Date)))
		b.WriteString("\n")
	}
	if prog.Credits != "" {
		s.writeXMLCredits(b, prog.Credits)
	}
	if prog.Category != "" {
		b.WriteString(fmt.Sprintf(`    <category>%s</category>`, xmlutil.XmlEscape(prog.Category)))
		b.WriteString("\n")
	}
	if prog.SubCategories != "" {
		var extras []string
		if json.Unmarshal([]byte(prog.SubCategories), &extras) == nil {
			for _, cat := range extras {
				b.WriteString(fmt.Sprintf(`    <category>%s</category>`, xmlutil.XmlEscape(cat)))
				b.WriteString("\n")
			}
		}
	}
	if prog.Language != "" {
		b.WriteString(fmt.Sprintf(`    <language>%s</language>`, xmlutil.XmlEscape(prog.Language)))
		b.WriteString("\n")
	}
	if prog.EpisodeNum != "" {
		system := prog.EpisodeNumSystem
		if system == "" {
			system = "onscreen"
		}
		b.WriteString(fmt.Sprintf(`    <episode-num system="%s">%s</episode-num>`, xmlutil.XmlEscape(system), xmlutil.XmlEscape(prog.EpisodeNum)))
		b.WriteString("\n")
	}
	if prog.Icon != "" {
		b.WriteString(fmt.Sprintf(`    <icon src="%s" />`, xmlutil.XmlEscape(prog.Icon)))
		b.WriteString("\n")
	}
	if prog.Rating != "" {
		b.WriteString(`    <rating>` + "\n")
		b.WriteString(fmt.Sprintf(`      <value>%s</value>`, xmlutil.XmlEscape(prog.Rating)))
		b.WriteString("\n")
		if prog.RatingIcon != "" {
			b.WriteString(fmt.Sprintf(`      <icon src="%s" />`, xmlutil.XmlEscape(prog.RatingIcon)))
			b.WriteString("\n")
		}
		b.WriteString(`    </rating>` + "\n")
	}
	if prog.StarRating != "" {
		b.WriteString(`    <star-rating>` + "\n")
		b.WriteString(fmt.Sprintf(`      <value>%s</value>`, xmlutil.XmlEscape(prog.StarRating)))
		b.WriteString("\n")
		b.WriteString(`    </star-rating>` + "\n")
	}
	if prog.IsPreviouslyShown {
		b.WriteString(`    <previously-shown />` + "\n")
	}
	if prog.IsNew {
		b.WriteString(`    <new />` + "\n")
	}

	b.WriteString("  </programme>\n")
}

func (s *OutputService) writeXMLCredits(b *strings.Builder, creditsJSON string) {
	var creds struct {
		Directors []string `json:"directors"`
		Actors    []string `json:"actors"`
		Writers   []string `json:"writers"`
	}
	if json.Unmarshal([]byte(creditsJSON), &creds) != nil {
		return
	}
	if len(creds.Directors) == 0 && len(creds.Actors) == 0 && len(creds.Writers) == 0 {
		return
	}
	b.WriteString(`    <credits>` + "\n")
	for _, d := range creds.Directors {
		b.WriteString(fmt.Sprintf(`      <director>%s</director>`, xmlutil.XmlEscape(d)))
		b.WriteString("\n")
	}
	for _, a := range creds.Actors {
		b.WriteString(fmt.Sprintf(`      <actor>%s</actor>`, xmlutil.XmlEscape(a)))
		b.WriteString("\n")
	}
	for _, w := range creds.Writers {
		b.WriteString(fmt.Sprintf(`      <writer>%s</writer>`, xmlutil.XmlEscape(w)))
		b.WriteString("\n")
	}
	b.WriteString(`    </credits>` + "\n")
}
