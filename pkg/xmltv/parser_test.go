package xmltv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="channel1">
    <display-name>Channel One</display-name>
    <icon src="http://example.com/icon1.png"/>
  </channel>
  <channel id="channel2">
    <display-name>Channel Two</display-name>
  </channel>
  <programme start="20240101120000 +0000" stop="20240101130000 +0000" channel="channel1">
    <title>Test Show</title>
    <desc>A test show description</desc>
    <category>News</category>
    <episode-num system="onscreen">S01E01</episode-num>
    <icon src="http://example.com/show.png"/>
  </programme>
</tv>`

	tv, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, tv.Channels, 2)
	require.Len(t, tv.Programmes, 1)

	assert.Equal(t, "channel1", tv.Channels[0].ID)
	assert.Equal(t, "Channel One", tv.Channels[0].DisplayName)
	assert.Equal(t, "http://example.com/icon1.png", tv.Channels[0].Icon)

	assert.Equal(t, "channel2", tv.Channels[1].ID)
	assert.Equal(t, "", tv.Channels[1].Icon)

	prog := tv.Programmes[0]
	assert.Equal(t, "channel1", prog.Channel)
	assert.Equal(t, "Test Show", prog.Title)
	assert.Equal(t, "A test show description", prog.Description)
	assert.Equal(t, "News", prog.Category)
	assert.Equal(t, "S01E01", prog.EpisodeNum)
	assert.Equal(t, "http://example.com/show.png", prog.Icon)
	assert.Equal(t, 2024, prog.Start.Year())
	assert.Equal(t, 12, prog.Start.Hour())
	assert.Equal(t, 13, prog.Stop.Hour())
}
