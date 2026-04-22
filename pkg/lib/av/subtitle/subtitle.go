// Package subtitle converts text subtitle data from demuxed packets into WebVTT
// format suitable for HTML5 <track> elements.
package subtitle

import (
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Cue represents a single subtitle entry with timing.
type Cue struct {
	StartMs int64  // start time in milliseconds
	EndMs   int64  // end time in milliseconds
	Text    string // subtitle text (may contain line breaks)
}

// Writer accumulates subtitle cues and writes WebVTT output.
type Writer struct {
	cues []Cue
}

// NewWriter creates a new WebVTT writer.
func NewWriter() *Writer {
	return &Writer{}
}

// AddCue adds a subtitle cue. PTS and duration are in nanoseconds
// (matching the avdemux.Packet timestamp format).
func (w *Writer) AddCue(ptsNs, durationNs int64, text string) {
	startMs := ptsNs / 1_000_000
	endMs := (ptsNs + durationNs) / 1_000_000
	w.cues = append(w.cues, Cue{
		StartMs: startMs,
		EndMs:   endMs,
		Text:    text,
	})
}

// WriteTo writes the accumulated cues as a WebVTT file.
func (w *Writer) WriteTo(out io.Writer) (int64, error) {
	var buf strings.Builder
	buf.WriteString("WEBVTT\n")
	for _, c := range w.cues {
		buf.WriteString("\n")
		buf.WriteString(FormatTimestamp(c.StartMs))
		buf.WriteString(" --> ")
		buf.WriteString(FormatTimestamp(c.EndMs))
		buf.WriteString("\n")
		buf.WriteString(c.Text)
		buf.WriteString("\n")
	}
	n, err := io.WriteString(out, buf.String())
	return int64(n), err
}

// srtTagRe matches HTML-like tags found in SRT subtitle data.
var srtTagRe = regexp.MustCompile(`<[^>]+>`)

// assPositionInSRTRe matches ASS-style override tags that sometimes appear in SRT data.
var assPositionInSRTRe = regexp.MustCompile(`\{\\[^}]*\}`)

// ConvertSRT converts SRT-format text (from subrip codec packets) to plain text.
// Strips SRT formatting tags like <b>, <i>, <font>, and position tags.
func ConvertSRT(srt string) string {
	// Remove ASS-style tags that sometimes appear in SRT (e.g. {\an8})
	result := assPositionInSRTRe.ReplaceAllString(srt, "")
	// Remove HTML-like tags
	result = srtTagRe.ReplaceAllString(result, "")
	return result
}

// assOverrideRe matches ASS/SSA override blocks: {\...}
var assOverrideRe = regexp.MustCompile(`\{\\[^}]*\}`)

// ConvertASS converts ASS/SSA subtitle text to plain text.
// Strips ASS override tags like {\an8}, {\pos(x,y)}, {\b1}, etc.
func ConvertASS(ass string) string {
	return assOverrideRe.ReplaceAllString(ass, "")
}

// FormatTimestamp converts milliseconds to WebVTT timestamp format "HH:MM:SS.mmm".
func FormatTimestamp(ms int64) string {
	h := ms / 3600000
	m := (ms % 3600000) / 60000
	s := (ms % 60000) / 1000
	ms = ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}
