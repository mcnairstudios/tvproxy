package xmltv

import (
	"encoding/xml"
	"io"
	"time"
)

type TV struct {
	Channels   []Channel
	Programmes []Programme
}

type Channel struct {
	ID          string
	DisplayName string
	Icon        string
}

type Programme struct {
	Channel     string
	Start       time.Time
	Stop        time.Time
	Title       string
	Description string
	Category    string
	EpisodeNum  string
	Icon        string
}

// Internal XML structures for parsing
type xmlTV struct {
	XMLName    xml.Name       `xml:"tv"`
	Channels   []xmlChannel   `xml:"channel"`
	Programmes []xmlProgramme `xml:"programme"`
}

type xmlChannel struct {
	ID          string    `xml:"id,attr"`
	DisplayName []xmlText `xml:"display-name"`
	Icon        *xmlIcon  `xml:"icon"`
}

type xmlProgramme struct {
	Start      string       `xml:"start,attr"`
	Stop       string       `xml:"stop,attr"`
	Channel    string       `xml:"channel,attr"`
	Title      []xmlText    `xml:"title"`
	Desc       []xmlText    `xml:"desc"`
	Category   []xmlText    `xml:"category"`
	EpisodeNum []xmlEpisode `xml:"episode-num"`
	Icon       *xmlIcon     `xml:"icon"`
}

type xmlText struct {
	Value string `xml:",chardata"`
}

type xmlIcon struct {
	Src string `xml:"src,attr"`
}

type xmlEpisode struct {
	System string `xml:"system,attr"`
	Value  string `xml:",chardata"`
}

const xmltvTimeFormat = "20060102150405 -0700"

func Parse(r io.Reader) (*TV, error) {
	var raw xmlTV
	if err := xml.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}

	tv := &TV{}
	for _, ch := range raw.Channels {
		c := Channel{ID: ch.ID}
		if len(ch.DisplayName) > 0 {
			c.DisplayName = ch.DisplayName[0].Value
		}
		if ch.Icon != nil {
			c.Icon = ch.Icon.Src
		}
		tv.Channels = append(tv.Channels, c)
	}

	for _, p := range raw.Programmes {
		prog := Programme{Channel: p.Channel}
		if len(p.Title) > 0 {
			prog.Title = p.Title[0].Value
		}
		if len(p.Desc) > 0 {
			prog.Description = p.Desc[0].Value
		}
		if len(p.Category) > 0 {
			prog.Category = p.Category[0].Value
		}
		if len(p.EpisodeNum) > 0 {
			prog.EpisodeNum = p.EpisodeNum[0].Value
		}
		if p.Icon != nil {
			prog.Icon = p.Icon.Src
		}

		if t, err := time.Parse(xmltvTimeFormat, p.Start); err == nil {
			prog.Start = t
		}
		if t, err := time.Parse(xmltvTimeFormat, p.Stop); err == nil {
			prog.Stop = t
		}

		tv.Programmes = append(tv.Programmes, prog)
	}
	return tv, nil
}
