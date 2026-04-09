package service

import (
	"context"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type ExportData struct {
	Version        int                    `json:"version"`
	Scope          string                 `json:"scope"`
	ExportedAt     time.Time              `json:"exported_at"`
	ChannelGroups  []models.ChannelGroup  `json:"channel_groups,omitempty"`
	Channels       []ExportChannel        `json:"channels,omitempty"`
	StreamProfiles []models.StreamProfile `json:"stream_profiles,omitempty"`
	SourceProfiles []models.SourceProfile `json:"source_profiles,omitempty"`
	Clients        []models.Client        `json:"clients,omitempty"`
	Settings       []models.CoreSetting   `json:"settings,omitempty"`
	Users          []ExportUser           `json:"users,omitempty"`
	M3UAccounts    []models.M3UAccount    `json:"m3u_accounts,omitempty"`
	EPGSources     []models.EPGSource     `json:"epg_sources,omitempty"`
}

type ExportChannel struct {
	Name        string   `json:"name"`
	TvgID       string   `json:"tvg_id,omitempty"`
	GroupName   string   `json:"group_name,omitempty"`
	ProfileName string   `json:"profile_name,omitempty"`
	IsEnabled   bool     `json:"is_enabled"`
	StreamIDs   []string `json:"stream_ids,omitempty"`
}

type ExportUser struct {
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

type ExportService struct {
	channelStore       store.ChannelStore
	channelGroupStore  store.ChannelGroupStore
	streamProfileRepo  store.ProfileStore
	sourceProfileStore store.SourceProfileStore
	clientStore        store.ClientStore
	m3uAccountStore    store.M3UAccountStore
	epgSourceStore     store.EPGSourceStore
	settingsService    *SettingsService
	authService        *AuthService
}

func NewExportService(
	channelStore store.ChannelStore,
	channelGroupStore store.ChannelGroupStore,
	streamProfileRepo store.ProfileStore,
	sourceProfileStore store.SourceProfileStore,
	clientStore store.ClientStore,
	m3uAccountStore store.M3UAccountStore,
	epgSourceStore store.EPGSourceStore,
	settingsService *SettingsService,
	authService *AuthService,
) *ExportService {
	return &ExportService{
		channelStore:       channelStore,
		channelGroupStore:  channelGroupStore,
		streamProfileRepo:  streamProfileRepo,
		sourceProfileStore: sourceProfileStore,
		clientStore:        clientStore,
		m3uAccountStore:    m3uAccountStore,
		epgSourceStore:     epgSourceStore,
		settingsService:    settingsService,
		authService:       authService,
	}
}

func (s *ExportService) Export(ctx context.Context, scope string) (*ExportData, error) {
	data := &ExportData{
		Version:    1,
		Scope:      scope,
		ExportedAt: time.Now(),
	}

	groups, err := s.channelGroupStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("exporting channel groups: %w", err)
	}
	data.ChannelGroups = groups

	groupNameMap := make(map[string]string, len(groups))
	for _, g := range groups {
		groupNameMap[g.ID] = g.Name
	}

	channels, err := s.channelStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("exporting channels: %w", err)
	}

	profileNameMap := make(map[string]string)
	if scope == "full" {
		profiles, err := s.streamProfileRepo.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("exporting stream profiles: %w", err)
		}
		data.StreamProfiles = profiles
		for _, p := range profiles {
			profileNameMap[p.ID] = p.Name
		}
		if s.sourceProfileStore != nil {
			sourceProfiles, err := s.sourceProfileStore.List(ctx)
			if err == nil {
				data.SourceProfiles = sourceProfiles
			}
		}
	}

	for _, ch := range channels {
		ec := ExportChannel{
			Name:      ch.Name,
			TvgID:     ch.TvgID,
			IsEnabled: ch.IsEnabled,
		}
		if ch.ChannelGroupID != nil {
			ec.GroupName = groupNameMap[*ch.ChannelGroupID]
		}
		if ch.StreamProfileID != nil {
			ec.ProfileName = profileNameMap[*ch.StreamProfileID]
		}
		streams, _ := s.channelStore.GetStreams(ctx, ch.ID)
		for _, st := range streams {
			ec.StreamIDs = append(ec.StreamIDs, st.StreamID)
		}
		data.Channels = append(data.Channels, ec)
	}

	if scope == "full" {
		clients, err := s.clientStore.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("exporting clients: %w", err)
		}
		data.Clients = clients

		settings, err := s.settingsService.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("exporting settings: %w", err)
		}
		data.Settings = settings

		users, err := s.authService.ListUsers(ctx)
		if err != nil {
			return nil, fmt.Errorf("exporting users: %w", err)
		}
		for _, u := range users {
			data.Users = append(data.Users, ExportUser{Username: u.Username, IsAdmin: u.IsAdmin})
		}

		accounts, err := s.m3uAccountStore.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("exporting m3u accounts: %w", err)
		}
		data.M3UAccounts = accounts

		sources, err := s.epgSourceStore.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("exporting epg sources: %w", err)
		}
		data.EPGSources = sources
	}

	return data, nil
}

func (s *ExportService) Import(ctx context.Context, data *ExportData) (int, error) {
	if data.Version != 1 {
		return 0, fmt.Errorf("unsupported export version: %d", data.Version)
	}

	var imported int

	existingGroups, _ := s.channelGroupStore.List(ctx)
	groupNameToID := make(map[string]string)
	for _, g := range existingGroups {
		groupNameToID[g.Name] = g.ID
	}
	for _, g := range data.ChannelGroups {
		if _, exists := groupNameToID[g.Name]; exists {
			continue
		}
		ng := &models.ChannelGroup{Name: g.Name, SortOrder: g.SortOrder, IsEnabled: g.IsEnabled}
		if err := s.channelGroupStore.Create(ctx, ng); err == nil {
			groupNameToID[ng.Name] = ng.ID
			imported++
		}
	}

	profileNameToID := make(map[string]string)
	if len(data.StreamProfiles) > 0 {
		existingProfiles, _ := s.streamProfileRepo.List(ctx)
		for _, p := range existingProfiles {
			profileNameToID[p.Name] = p.ID
		}
		for _, p := range data.StreamProfiles {
			if _, exists := profileNameToID[p.Name]; exists {
				continue
			}
			if p.IsSystem || p.IsClient {
				continue
			}
			np := &models.StreamProfile{
				Name: p.Name, StreamMode: p.StreamMode,
				HWAccel: p.HWAccel, VideoCodec: p.VideoCodec, Container: p.Container,
				Deinterlace: p.Deinterlace, FPSMode: p.FPSMode,
				UseCustomArgs: p.UseCustomArgs, CustomArgs: p.CustomArgs,
				Command: p.Command, Args: p.Args,
			}
			if err := s.streamProfileRepo.Create(ctx, np); err == nil {
				profileNameToID[np.Name] = np.ID
				imported++
			}
		}
	}

	existingChannels, _ := s.channelStore.List(ctx)
	channelNameSet := make(map[string]bool, len(existingChannels))
	for _, c := range existingChannels {
		channelNameSet[c.Name] = true
	}
	for _, ec := range data.Channels {
		if channelNameSet[ec.Name] {
			continue
		}
		ch := &models.Channel{
			Name:      ec.Name,
			TvgID:     ec.TvgID,
			IsEnabled: ec.IsEnabled,
		}
		if ec.GroupName != "" {
			if gid, ok := groupNameToID[ec.GroupName]; ok {
				ch.ChannelGroupID = &gid
			}
		}
		if ec.ProfileName != "" {
			if pid, ok := profileNameToID[ec.ProfileName]; ok {
				ch.StreamProfileID = &pid
			}
		}
		if err := s.channelStore.Create(ctx, ch); err == nil {
			imported++
			if len(ec.StreamIDs) > 0 {
				priorities := make([]int, len(ec.StreamIDs))
				for i := range priorities {
					priorities[i] = i
				}
				s.channelStore.AssignStreams(ctx, ch.ID, ec.StreamIDs, priorities)
			}
		}
	}

	if len(data.Settings) > 0 {
		for _, st := range data.Settings {
			s.settingsService.Set(ctx, st.Key, st.Value)
			imported++
		}
	}

	if len(data.M3UAccounts) > 0 {
		existingAccounts, _ := s.m3uAccountStore.List(ctx)
		accountNameSet := make(map[string]bool, len(existingAccounts))
		for _, a := range existingAccounts {
			accountNameSet[a.Name] = true
		}
		for _, a := range data.M3UAccounts {
			if accountNameSet[a.Name] {
				continue
			}
			na := &models.M3UAccount{
				Name: a.Name, URL: a.URL, Type: a.Type,
				Username: a.Username, Password: a.Password,
				MaxStreams: a.MaxStreams, IsEnabled: a.IsEnabled,
				RefreshInterval: a.RefreshInterval,
			}
			if err := s.m3uAccountStore.Create(ctx, na); err == nil {
				imported++
			}
		}
	}

	if len(data.EPGSources) > 0 {
		existingSources, _ := s.epgSourceStore.List(ctx)
		sourceNameSet := make(map[string]bool, len(existingSources))
		for _, st := range existingSources {
			sourceNameSet[st.Name] = true
		}
		for _, st := range data.EPGSources {
			if sourceNameSet[st.Name] {
				continue
			}
			ns := &models.EPGSource{Name: st.Name, URL: st.URL, IsEnabled: st.IsEnabled}
			if err := s.epgSourceStore.Create(ctx, ns); err == nil {
				imported++
			}
		}
	}

	return imported, nil
}
