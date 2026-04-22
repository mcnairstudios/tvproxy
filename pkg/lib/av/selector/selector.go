package selector

// AudioTrack mirrors the probe result for an audio stream.
type AudioTrack struct {
	Index       int
	Codec       string
	Channels    int
	SampleRate  int
	Language    string
	IsAD        bool
	BitrateKbps int
}

// AudioPrefs configures audio track selection priorities.
type AudioPrefs struct {
	Language string // preferred ISO 639 language code (e.g. "en")
}

// codecPriority returns a score for the given codec name.
// Higher is better. AAC is preferred because it can be passed through
// to CMAF segments without transcoding.
func codecPriority(codec string) int {
	switch codec {
	case "aac":
		return 50
	case "mp2":
		return 40
	case "ac3":
		return 30
	case "eac3":
		return 20
	case "dts":
		return 10
	default:
		return 0
	}
}

// SelectAudio picks the best audio track from available tracks.
//
// Selection priority:
//  1. Skip AD (audio description) tracks unless they are all AD.
//  2. Match preferred language (if set).
//  3. Among matching tracks, prefer AAC over other codecs (less CPU for passthrough).
//  4. Among same codec, prefer higher channel count (5.1 > stereo).
//  5. Fall back to first non-AD track, then first track.
//
// Returns the selected track's Index, or -1 if no tracks are available.
func SelectAudio(tracks []AudioTrack, prefs AudioPrefs) int {
	if len(tracks) == 0 {
		return -1
	}

	// Filter out AD tracks unless all are AD.
	candidates := make([]AudioTrack, 0, len(tracks))
	for _, t := range tracks {
		if !t.IsAD {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		candidates = tracks
	}

	// If a language preference is set, narrow to matching tracks.
	if prefs.Language != "" {
		langMatches := make([]AudioTrack, 0, len(candidates))
		for _, t := range candidates {
			if t.Language == prefs.Language {
				langMatches = append(langMatches, t)
			}
		}
		if len(langMatches) > 0 {
			candidates = langMatches
		}
	}

	// Pick the best candidate by codec priority then channel count.
	best := candidates[0]
	for _, t := range candidates[1:] {
		tp := codecPriority(t.Codec)
		bp := codecPriority(best.Codec)
		if tp > bp || (tp == bp && t.Channels > best.Channels) {
			best = t
		}
	}

	return best.Index
}
