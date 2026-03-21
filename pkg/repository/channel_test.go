package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

func createTestUser(t *testing.T, db *database.DB) string {
	t.Helper()
	repo := NewUserRepository(db)
	user := &models.User{Username: "testowner", PasswordHash: "hash", IsAdmin: true}
	require.NoError(t, repo.Create(context.Background(), user))
	return user.ID
}

func TestChannelCRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := NewChannelRepository(db)
	logoRepo := NewLogoRepository(db)
	ctx := context.Background()
	userID := createTestUser(t, db)

	// Create a logo first
	logo := &models.Logo{Name: "Test Logo", URL: "http://example.com/logo.png"}
	err := logoRepo.Create(ctx, logo)
	require.NoError(t, err)

	// Create
	channel := &models.Channel{
		UserID:    userID,
		Name:      "Test Channel",
		LogoID:    &logo.ID,
		TvgID:     "test.channel",
		IsEnabled: true,
	}
	err = repo.Create(ctx, channel)
	require.NoError(t, err)
	assert.NotZero(t, channel.ID)
	assert.False(t, channel.CreatedAt.IsZero())
	assert.False(t, channel.UpdatedAt.IsZero())

	// Read by ID
	fetched, err := repo.GetByID(ctx, channel.ID)
	require.NoError(t, err)
	assert.Equal(t, channel.ID, fetched.ID)
	assert.Equal(t, "Test Channel", fetched.Name)
	require.NotNil(t, fetched.LogoID)
	assert.Equal(t, logo.ID, *fetched.LogoID)
	assert.Equal(t, "http://example.com/logo.png", fetched.Logo)
	assert.Equal(t, "test.channel", fetched.TvgID)
	assert.True(t, fetched.IsEnabled)
	assert.Nil(t, fetched.ChannelGroupID)

	// Update logo URL
	logo2 := &models.Logo{Name: "New Logo", URL: "http://example.com/newlogo.png"}
	err = logoRepo.Create(ctx, logo2)
	require.NoError(t, err)

	channel.Name = "Updated Channel"
	channel.LogoID = &logo2.ID
	channel.TvgID = "updated.channel"
	channel.IsEnabled = false
	err = repo.Update(ctx, channel)
	require.NoError(t, err)

	fetched, err = repo.GetByID(ctx, channel.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Channel", fetched.Name)
	assert.Equal(t, "http://example.com/newlogo.png", fetched.Logo)
	assert.Equal(t, "updated.channel", fetched.TvgID)
	assert.False(t, fetched.IsEnabled)

	// List
	channel2 := &models.Channel{
		UserID:    userID,
		Name:      "Channel Two",
		IsEnabled: true,
	}
	err = repo.Create(ctx, channel2)
	require.NoError(t, err)

	channels, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, channels, 2)

	// Delete
	err = repo.Delete(ctx, channel.ID)
	require.NoError(t, err)

	_, err = repo.GetByID(ctx, channel.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "channel not found")

	channels, err = repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, channels, 1)
	assert.Equal(t, "Channel Two", channels[0].Name)
}

func TestChannelGetByIDNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewChannelRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "channel not found")
}

func TestChannelWithGroup(t *testing.T) {
	db := setupTestDB(t)
	channelRepo := NewChannelRepository(db)
	groupRepo := NewChannelGroupRepository(db)
	ctx := context.Background()
	userID := createTestUser(t, db)

	group := &models.ChannelGroup{
		UserID:    userID,
		Name:      "Sports",
		IsEnabled: true,
		SortOrder: 1,
	}
	err := groupRepo.Create(ctx, group)
	require.NoError(t, err)

	channel := &models.Channel{
		UserID:         userID,
		Name:           "ESPN",
		IsEnabled:      true,
		ChannelGroupID: &group.ID,
	}
	err = channelRepo.Create(ctx, channel)
	require.NoError(t, err)

	fetched, err := channelRepo.GetByID(ctx, channel.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched.ChannelGroupID)
	assert.Equal(t, group.ID, *fetched.ChannelGroupID)
}

func TestChannelAssignStreams(t *testing.T) {
	db := setupTestDB(t)
	channelRepo := NewChannelRepository(db)
	m3uRepo := NewM3UAccountRepository(db)
	streamRepo := NewStreamRepository(db)
	ctx := context.Background()
	userID := createTestUser(t, db)

	// Create an M3U account (needed as FK for streams)
	account := &models.M3UAccount{
		Name:       "Test Account",
		URL:        "http://example.com/playlist.m3u",
		Type:       "m3u",
		MaxStreams: 1,
		IsEnabled:  true,
	}
	err := m3uRepo.Create(ctx, account)
	require.NoError(t, err)

	// Create some streams
	stream1 := &models.Stream{
		M3UAccountID: account.ID,
		Name:         "Stream One",
		URL:          "http://example.com/stream1.ts",
		Group:        "Sports",
		IsActive:     true,
	}
	err = streamRepo.Create(ctx, stream1)
	require.NoError(t, err)

	stream2 := &models.Stream{
		M3UAccountID: account.ID,
		Name:         "Stream Two",
		URL:          "http://example.com/stream2.ts",
		Group:        "Sports",
		IsActive:     true,
	}
	err = streamRepo.Create(ctx, stream2)
	require.NoError(t, err)

	// Create a channel
	channel := &models.Channel{
		UserID:    userID,
		Name:      "Sports Channel",
		IsEnabled: true,
	}
	err = channelRepo.Create(ctx, channel)
	require.NoError(t, err)

	// Assign streams with priorities
	err = channelRepo.AssignStreams(ctx, channel.ID, []string{stream1.ID, stream2.ID}, []int{1, 2})
	require.NoError(t, err)

	// Verify stream assignments
	channelStreams, err := channelRepo.GetStreams(ctx, channel.ID)
	require.NoError(t, err)
	assert.Len(t, channelStreams, 2)
	assert.Equal(t, stream1.ID, channelStreams[0].StreamID)
	assert.Equal(t, 1, channelStreams[0].Priority)
	assert.Equal(t, stream2.ID, channelStreams[1].StreamID)
	assert.Equal(t, 2, channelStreams[1].Priority)

	// Re-assign with different streams/priorities (should replace)
	err = channelRepo.AssignStreams(ctx, channel.ID, []string{stream2.ID}, []int{1})
	require.NoError(t, err)

	channelStreams, err = channelRepo.GetStreams(ctx, channel.ID)
	require.NoError(t, err)
	assert.Len(t, channelStreams, 1)
	assert.Equal(t, stream2.ID, channelStreams[0].StreamID)
	assert.Equal(t, 1, channelStreams[0].Priority)
}

func TestChannelAssignStreamsMismatchedLengths(t *testing.T) {
	db := setupTestDB(t)
	repo := NewChannelRepository(db)
	ctx := context.Background()

	err := repo.AssignStreams(ctx, "some-id", []string{"a", "b"}, []int{1})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "same length")
}

func TestChannelAssignStreamsEmpty(t *testing.T) {
	db := setupTestDB(t)
	channelRepo := NewChannelRepository(db)
	m3uRepo := NewM3UAccountRepository(db)
	streamRepo := NewStreamRepository(db)
	ctx := context.Background()
	userID := createTestUser(t, db)

	// Create prerequisites
	account := &models.M3UAccount{
		Name:       "Test Account",
		URL:        "http://example.com/playlist.m3u",
		Type:       "m3u",
		MaxStreams: 1,
		IsEnabled:  true,
	}
	err := m3uRepo.Create(ctx, account)
	require.NoError(t, err)

	stream1 := &models.Stream{
		M3UAccountID: account.ID,
		Name:         "Stream One",
		URL:          "http://example.com/stream1.ts",
		IsActive:     true,
	}
	err = streamRepo.Create(ctx, stream1)
	require.NoError(t, err)

	channel := &models.Channel{
		UserID:    userID,
		Name:      "Test Channel",
		IsEnabled: true,
	}
	err = channelRepo.Create(ctx, channel)
	require.NoError(t, err)

	// Assign a stream first
	err = channelRepo.AssignStreams(ctx, channel.ID, []string{stream1.ID}, []int{1})
	require.NoError(t, err)

	// Now assign empty to clear all assignments
	err = channelRepo.AssignStreams(ctx, channel.ID, []string{}, []int{})
	require.NoError(t, err)

	channelStreams, err := channelRepo.GetStreams(ctx, channel.ID)
	require.NoError(t, err)
	assert.Empty(t, channelStreams)
}

func TestChannelGetStreamsEmpty(t *testing.T) {
	db := setupTestDB(t)
	channelRepo := NewChannelRepository(db)
	ctx := context.Background()
	userID := createTestUser(t, db)

	channel := &models.Channel{
		UserID:    userID,
		Name:      "Empty Channel",
		IsEnabled: true,
	}
	err := channelRepo.Create(ctx, channel)
	require.NoError(t, err)

	streams, err := channelRepo.GetStreams(ctx, channel.ID)
	require.NoError(t, err)
	assert.Empty(t, streams)
}

func TestChannelListEmpty(t *testing.T) {
	db := setupTestDB(t)
	repo := NewChannelRepository(db)
	ctx := context.Background()

	channels, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, channels)
}
