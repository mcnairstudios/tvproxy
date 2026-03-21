package service

import "fmt"

func ResolveChannelURL(channelID string, baseURL string) string {
	return fmt.Sprintf("%s/channel/%s", baseURL, channelID)
}
