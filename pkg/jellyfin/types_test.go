package jellyfin

import (
	"encoding/json"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestBaseItemDto_MovieListItem_RequiredFields(t *testing.T) {
	item := BaseItemDto{
		Name:              "3 10 to Yuma",
		ServerID:          "61da34e701b54548a25c783de5e13284",
		ID:                "f842a4f8d071cf6b33e02873ba437ed7",
		HasSubtitles:      true,
		Container:         "mkv",
		PremiereDate:      "2021-04-23T10:03:17.0000000Z",
		ChannelID:         nil,
		RunTimeTicks:      73454720000,
		ProductionYear:    2007,
		IsFolder:          false,
		Type:              "Movie",
		VideoType:         "VideoFile",
		ImageTags:         map[string]string{},
		BackdropImageTags: []string{},
		ImageBlurHashes:   map[string]any{},
		LocationType:      "FileSystem",
		MediaType:         "Video",
		UserData: &UserItemData{
			PlaybackPositionTicks: 0,
			PlayCount:             0,
			IsFavorite:            false,
			Played:                false,
			Key:                   "f842a4f8-d071-cf6b-33e0-2873ba437ed7",
			ItemID:                "f842a4f8d071cf6b33e02873ba437ed7",
		},
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map failed: %v", err)
	}

	requiredFields := []string{
		"Name", "ServerId", "Id", "Container", "ChannelId",
		"RunTimeTicks", "ProductionYear", "IsFolder", "Type",
		"VideoType", "ImageTags", "BackdropImageTags", "ImageBlurHashes",
		"LocationType", "MediaType", "UserData",
	}
	for _, field := range requiredFields {
		if _, ok := m[field]; !ok {
			t.Errorf("required field %q missing from serialized output", field)
		}
	}

	if m["ChannelId"] != nil {
		t.Errorf("ChannelId should serialize as null, got %v", m["ChannelId"])
	}

	if _, ok := m["Width"]; ok {
		t.Error("Width should not be present in serialized output")
	}
	if _, ok := m["Height"]; ok {
		t.Error("Height should not be present in serialized output")
	}

	if v, ok := m["HasSubtitles"]; !ok || v != true {
		t.Errorf("HasSubtitles should be true, got %v", v)
	}

	userData, ok := m["UserData"].(map[string]any)
	if !ok {
		t.Fatal("UserData should be an object")
	}
	if _, ok := userData["ItemId"]; !ok {
		t.Error("UserData.ItemId should always be present")
	}

	if v, ok := m["ImageTags"]; !ok {
		t.Error("ImageTags should always be present even when empty")
	} else if v == nil {
		t.Error("ImageTags should be {} not null")
	}

	if v, ok := m["BackdropImageTags"]; !ok {
		t.Error("BackdropImageTags should always be present even when empty")
	} else if v == nil {
		t.Error("BackdropImageTags should be [] not null")
	}

	if v, ok := m["ImageBlurHashes"]; !ok {
		t.Error("ImageBlurHashes should always be present even when empty")
	} else if v == nil {
		t.Error("ImageBlurHashes should be {} not null")
	}
}

func TestBaseItemDto_NoSubtitles_HasSubtitlesAbsent(t *testing.T) {
	item := BaseItemDto{
		Name:              "1917",
		ServerID:          "61da34e701b54548a25c783de5e13284",
		ID:                "b6a24780f45e58a9ffcaa6a03c06b766",
		Container:         "mkv",
		ChannelID:         nil,
		RunTimeTicks:      71394830000,
		ProductionYear:    2019,
		IsFolder:          false,
		Type:              "Movie",
		VideoType:         "VideoFile",
		ImageTags:         map[string]string{},
		BackdropImageTags: []string{},
		ImageBlurHashes:   map[string]any{},
		LocationType:      "FileSystem",
		MediaType:         "Video",
		UserData: &UserItemData{
			Key:    "b6a24780-f45e-58a9-ffca-a6a03c06b766",
			ItemID: "b6a24780f45e58a9ffcaa6a03c06b766",
		},
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if _, ok := m["HasSubtitles"]; ok {
		t.Error("HasSubtitles should be absent when false (omitempty)")
	}
}

func TestBaseItemDto_ChannelIdNull(t *testing.T) {
	item := BaseItemDto{
		Name:              "Test",
		ServerID:          "server1",
		ID:                "item1",
		ChannelID:         nil,
		Type:              "Movie",
		ImageTags:         map[string]string{},
		BackdropImageTags: []string{},
		ImageBlurHashes:   map[string]any{},
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	v, ok := m["ChannelId"]
	if !ok {
		t.Error("ChannelId key must always be present")
	}
	if v != nil {
		t.Errorf("ChannelId with nil pointer should serialize as null, got %v", v)
	}
}

func TestUserItemData_ItemIdAlwaysPresent(t *testing.T) {
	u := UserItemData{
		PlaybackPositionTicks: 0,
		PlayCount:             0,
		IsFavorite:            false,
		Played:                false,
		Key:                   "some-key",
		ItemID:                "itemid123",
	}

	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if _, ok := m["ItemId"]; !ok {
		t.Error("ItemId must always be present in UserItemData")
	}
}

func TestBaseItemDto_CanDelete_Omitempty(t *testing.T) {
	itemWithoutDelete := BaseItemDto{
		Name:              "Test",
		ServerID:          "server1",
		ID:                "item1",
		ChannelID:         nil,
		Type:              "Movie",
		ImageTags:         map[string]string{},
		BackdropImageTags: []string{},
		ImageBlurHashes:   map[string]any{},
	}

	data, err := json.Marshal(itemWithoutDelete)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if _, ok := m["CanDelete"]; ok {
		t.Error("CanDelete should be absent when false (omitempty)")
	}

	itemWithDelete := itemWithoutDelete
	itemWithDelete.CanDelete = boolPtr(true)

	data2, err := json.Marshal(itemWithDelete)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m2 map[string]any
	if err := json.Unmarshal(data2, &m2); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if v, ok := m2["CanDelete"]; !ok || v != true {
		t.Errorf("CanDelete should be present and true when set, got %v", v)
	}
}

func TestBaseItemDto_RoundTrip_ListItem(t *testing.T) {
	raw := `{"Name":"3 10 to Yuma","ServerId":"61da34e701b54548a25c783de5e13284","Id":"f842a4f8d071cf6b33e02873ba437ed7","HasSubtitles":true,"Container":"mkv","PremiereDate":"2021-04-23T10:03:17.0000000Z","ChannelId":null,"RunTimeTicks":73454720000,"ProductionYear":2007,"IsFolder":false,"Type":"Movie","UserData":{"PlaybackPositionTicks":0,"PlayCount":0,"IsFavorite":false,"Played":false,"Key":"f842a4f8-d071-cf6b-33e0-2873ba437ed7","ItemId":"f842a4f8d071cf6b33e02873ba437ed7"},"VideoType":"VideoFile","ImageTags":{},"BackdropImageTags":[],"ImageBlurHashes":{},"LocationType":"FileSystem","MediaType":"Video"}`

	var item BaseItemDto
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if item.Name != "3 10 to Yuma" {
		t.Errorf("Name: got %q", item.Name)
	}
	if item.ID != "f842a4f8d071cf6b33e02873ba437ed7" {
		t.Errorf("Id: got %q", item.ID)
	}
	if item.ChannelID != nil {
		t.Errorf("ChannelId should be nil, got %v", item.ChannelID)
	}
	if !item.HasSubtitles {
		t.Error("HasSubtitles should be true")
	}
	if item.VideoType != "VideoFile" {
		t.Errorf("VideoType: got %q", item.VideoType)
	}
	if item.UserData == nil {
		t.Fatal("UserData should not be nil")
	}
	if item.UserData.ItemID != "f842a4f8d071cf6b33e02873ba437ed7" {
		t.Errorf("UserData.ItemId: got %q", item.UserData.ItemID)
	}
}
