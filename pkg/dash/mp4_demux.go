package dash

import (
	"bytes"
	"fmt"

	"github.com/Eyevinn/mp4ff/mp4"
)

func filterInitForTrack(initData []byte, keepTrackID uint32) ([]byte, error) {
	sr := bytes.NewReader(initData)
	parsed, err := mp4.DecodeFile(sr)
	if err != nil {
		return nil, fmt.Errorf("decoding init: %w", err)
	}
	if parsed.Init == nil {
		return nil, fmt.Errorf("no init segment found")
	}

	moov := parsed.Init.Moov

	var keptTrak *mp4.TrakBox
	var newMoovChildren []mp4.Box
	for _, child := range moov.Children {
		switch c := child.(type) {
		case *mp4.TrakBox:
			if c.Tkhd.TrackID == keepTrackID {
				keptTrak = c
				newMoovChildren = append(newMoovChildren, child)
			}
		case *mp4.MvexBox:
			var mvexChildren []mp4.Box
			var keptTrexs []*mp4.TrexBox
			for _, mc := range c.Children {
				if trex, ok := mc.(*mp4.TrexBox); ok {
					if trex.TrackID == keepTrackID {
						keptTrexs = append(keptTrexs, trex)
						mvexChildren = append(mvexChildren, mc)
					}
				} else {
					mvexChildren = append(mvexChildren, mc)
				}
			}
			c.Children = mvexChildren
			c.Trexs = keptTrexs
			if len(keptTrexs) > 0 {
				c.Trex = keptTrexs[0]
			}
			newMoovChildren = append(newMoovChildren, child)
		default:
			newMoovChildren = append(newMoovChildren, child)
		}
	}

	moov.Children = newMoovChildren
	if keptTrak != nil {
		moov.Traks = []*mp4.TrakBox{keptTrak}
		moov.Trak = keptTrak
	} else {
		moov.Traks = nil
		moov.Trak = nil
	}

	var buf bytes.Buffer
	if err := parsed.Init.Encode(&buf); err != nil {
		return nil, fmt.Errorf("encoding filtered init: %w", err)
	}
	return buf.Bytes(), nil
}

func demuxSegment(segData []byte, parsedInit *mp4.InitSegment, keepTrackID uint32) ([]byte, error) {
	sr := bytes.NewReader(segData)
	parsed, err := mp4.DecodeFile(sr, mp4.WithDecodeFlags(mp4.DecStartOnMoof))
	if err != nil {
		return nil, fmt.Errorf("decoding segment: %w", err)
	}

	if len(parsed.Segments) == 0 || len(parsed.Segments[0].Fragments) == 0 {
		return nil, fmt.Errorf("no fragments in segment data")
	}

	frag := parsed.Segments[0].Fragments[0]

	trex, ok := parsedInit.Moov.Mvex.GetTrex(keepTrackID)
	if !ok {
		return nil, fmt.Errorf("no trex for track %d", keepTrackID)
	}

	samples, err := frag.GetFullSamples(trex)
	if err != nil {
		return nil, fmt.Errorf("extracting samples for track %d: %w", keepTrackID, err)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("no samples for track %d", keepTrackID)
	}

	seqNum := frag.Moof.Mfhd.SequenceNumber
	newFrag, err := mp4.CreateFragment(seqNum, keepTrackID)
	if err != nil {
		return nil, fmt.Errorf("creating fragment: %w", err)
	}

	for _, sample := range samples {
		if err := newFrag.AddFullSampleToTrack(sample, keepTrackID); err != nil {
			return nil, fmt.Errorf("adding sample: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := newFrag.Encode(&buf); err != nil {
		return nil, fmt.Errorf("encoding fragment: %w", err)
	}
	return buf.Bytes(), nil
}
