package service

import (
	"context"

	"github.com/gavinmcnair/tvproxy/pkg/defaults"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type clearable interface {
	ClearAndSave() error
}

type DataResetter struct {
	profileStore    *store.ProfileStoreImpl
	settingsStore   *store.SettingsStoreImpl
	clientStore     *store.ClientStoreImpl
	logoStore       *store.LogoStoreImpl
	m3uAccountStore *store.M3UAccountStoreImpl
	epgSourceStore  *store.EPGSourceStoreImpl
	hdhrStore       *store.HDHRDeviceStoreImpl
	userStore       *store.UserStoreImpl
	channelStore    *store.ChannelStoreImpl
	channelGroupSt  *store.ChannelGroupStoreImpl
	scheduledRecSt  *store.ScheduledRecordingStoreImpl
	clientDefs      *defaults.ClientDefaults
	seedFn          func()
}

func NewDataResetter(
	profileStore *store.ProfileStoreImpl,
	settingsStore *store.SettingsStoreImpl,
	clientStore *store.ClientStoreImpl,
	logoStore *store.LogoStoreImpl,
	m3uAccountStore *store.M3UAccountStoreImpl,
	epgSourceStore *store.EPGSourceStoreImpl,
	hdhrStore *store.HDHRDeviceStoreImpl,
	userStore *store.UserStoreImpl,
	channelStore *store.ChannelStoreImpl,
	channelGroupStore *store.ChannelGroupStoreImpl,
	scheduledRecStore *store.ScheduledRecordingStoreImpl,
	clientDefs *defaults.ClientDefaults,
	seedFn func(),
) *DataResetter {
	return &DataResetter{
		profileStore:    profileStore,
		settingsStore:   settingsStore,
		clientStore:     clientStore,
		logoStore:       logoStore,
		m3uAccountStore: m3uAccountStore,
		epgSourceStore:  epgSourceStore,
		hdhrStore:       hdhrStore,
		userStore:       userStore,
		channelStore:    channelStore,
		channelGroupSt:  channelGroupStore,
		scheduledRecSt:  scheduledRecStore,
		clientDefs:      clientDefs,
		seedFn:          seedFn,
	}
}

func (r *DataResetter) SoftReset() error {
	stores := []clearable{
		r.channelStore, r.channelGroupSt, r.logoStore,
		r.m3uAccountStore, r.epgSourceStore, r.hdhrStore,
		r.scheduledRecSt,
	}
	for _, s := range stores {
		if err := s.ClearAndSave(); err != nil {
			return err
		}
	}
	r.seedFn()
	return nil
}

func (r *DataResetter) HardReset() error {
	stores := []clearable{
		r.profileStore, r.settingsStore, r.clientStore,
		r.channelStore, r.channelGroupSt, r.logoStore,
		r.m3uAccountStore, r.epgSourceStore, r.hdhrStore,
		r.userStore, r.scheduledRecSt,
	}
	for _, s := range stores {
		if err := s.ClearAndSave(); err != nil {
			return err
		}
	}
	r.profileStore.SeedSystemProfiles()
	if err := r.profileStore.Save(); err != nil {
		return err
	}
	if r.clientDefs != nil {
		SeedClientDefaults(context.Background(), r.clientDefs, r.profileStore, r.clientStore)
	}
	return nil
}
