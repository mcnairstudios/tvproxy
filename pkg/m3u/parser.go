package m3u

import (
	"bufio"
	"io"
	"strings"
)

type Entry struct {
	Name       string
	URL        string
	Group      string
	Logo       string
	TvgID      string
	TvgName    string
	TVPType       string
	TVPSeries     string
	TVPCollection string
	TVPSeason     string
	TVPEpisode string
	TVPVCodec  string
	TVPACodec  string
	TVPRes     string
	TVPAudio   string
	TVPDur        string
	TVPTags       string
	TVPSeasonName string
}

func Parse(r io.Reader) ([]Entry, error) {
	var entries []Entry
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var current *Entry
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "#EXTM3U" {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			current = &Entry{}
			parseExtInf(line, current)
		} else if current != nil && !strings.HasPrefix(line, "#") {
			current.URL = line
			entries = append(entries, *current)
			current = nil
		}
	}
	return entries, scanner.Err()
}

func parseExtInf(line string, entry *Entry) {
	entry.TvgID = extractAttr(line, "tvg-id")
	entry.TvgName = extractAttr(line, "tvg-name")
	entry.Logo = extractAttr(line, "tvg-logo")
	entry.Group = extractAttr(line, "group-title")
	entry.TVPType = extractAttr(line, "tvp-type")
	entry.TVPSeries = extractAttr(line, "tvp-series")
	entry.TVPCollection = extractAttr(line, "tvp-collection")
	entry.TVPSeason = extractAttr(line, "tvp-season")
	entry.TVPEpisode = extractAttr(line, "tvp-episode")
	entry.TVPVCodec = extractAttr(line, "tvp-vcodec")
	entry.TVPACodec = extractAttr(line, "tvp-acodec")
	entry.TVPRes = extractAttr(line, "tvp-resolution")
	entry.TVPAudio = extractAttr(line, "tvp-audio")
	entry.TVPDur = extractAttr(line, "tvp-duration")
	entry.TVPTags = extractAttr(line, "tvp-tags")
	entry.TVPSeasonName = extractAttr(line, "tvp-season-name")

	if idx := strings.LastIndex(line, ","); idx >= 0 {
		entry.Name = strings.TrimSpace(line[idx+1:])
	}
}

func extractAttr(line, attr string) string {
	key := attr + `="`
	start := strings.Index(line, key)
	if start < 0 {
		return ""
	}
	start += len(key)
	end := strings.Index(line[start:], `"`)
	if end < 0 {
		return ""
	}
	return line[start : start+end]
}
