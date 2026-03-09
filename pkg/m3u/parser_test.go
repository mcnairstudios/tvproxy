package m3u

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	input := `#EXTM3U
#EXTINF:-1 tvg-id="channel1" tvg-name="Channel One" tvg-logo="http://example.com/logo1.png" group-title="News",Channel 1
http://example.com/stream1
#EXTINF:-1 tvg-id="channel2" tvg-name="Channel Two" tvg-logo="http://example.com/logo2.png" group-title="Sports",Channel 2
http://example.com/stream2
#EXTINF:-1 group-title="Movies",Channel 3
http://example.com/stream3
`
	entries, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, entries, 3)

	assert.Equal(t, "Channel 1", entries[0].Name)
	assert.Equal(t, "http://example.com/stream1", entries[0].URL)
	assert.Equal(t, "News", entries[0].Group)
	assert.Equal(t, "channel1", entries[0].TvgID)
	assert.Equal(t, "Channel One", entries[0].TvgName)
	assert.Equal(t, "http://example.com/logo1.png", entries[0].Logo)

	assert.Equal(t, "Channel 2", entries[1].Name)
	assert.Equal(t, "Sports", entries[1].Group)

	assert.Equal(t, "Channel 3", entries[2].Name)
	assert.Equal(t, "Movies", entries[2].Group)
	assert.Equal(t, "", entries[2].TvgID)
}

func TestParseEmpty(t *testing.T) {
	entries, err := Parse(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestParseHeaderOnly(t *testing.T) {
	entries, err := Parse(strings.NewReader("#EXTM3U\n"))
	require.NoError(t, err)
	assert.Empty(t, entries)
}
