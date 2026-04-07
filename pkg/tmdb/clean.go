package tmdb

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	editionTag = regexp.MustCompile(`\{[^}]+\}`)
	yearParen  = regexp.MustCompile(`\((\d{4})\)`)
)

func CleanVODName(name string) (clean string, year string) {
	cleaned := editionTag.ReplaceAllString(name, "")
	if m := yearParen.FindStringSubmatch(cleaned); len(m) > 1 {
		year = m[1]
		cleaned = yearParen.ReplaceAllString(cleaned, "")
	}
	return strings.TrimSpace(cleaned), year
}

func BuildQuery(name string) (query string, year string) {
	clean, yr := CleanVODName(name)
	return clean, yr
}

func SearchCacheKey(query, mediaType string) string {
	key := "search_" + query
	if mediaType != "" {
		key += "_" + mediaType
	}
	return key
}

func DetailCacheKey(mediaType, id string) string {
	return "detail_" + mediaType + "_" + id
}

func PosterURL(posterPath string) string {
	if posterPath == "" {
		return ""
	}
	return "/api/tmdb/image?path=" + url.QueryEscape(posterPath)
}
