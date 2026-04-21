package gstreamer

import "path/filepath"

type PipelineSpec struct {
	Elements []ElementSpec
	Links    []LinkSpec
}

type ElementSpec struct {
	Name       string
	Factory    string
	Properties map[string]any
}

type LinkSpec struct {
	FromElement string
	FromPad     string
	ToElement   string
	ToPad       string
}

type Props = map[string]any

func (s *PipelineSpec) AddElement(name, factory string, props Props) {
	s.Elements = append(s.Elements, ElementSpec{
		Name:       name,
		Factory:    factory,
		Properties: props,
	})
}

func (s *PipelineSpec) Link(from, to string) {
	s.Links = append(s.Links, LinkSpec{
		FromElement: from,
		ToElement:   to,
	})
}

func (s *PipelineSpec) LinkPads(from, fromPad, to, toPad string) {
	s.Links = append(s.Links, LinkSpec{
		FromElement: from,
		FromPad:     fromPad,
		ToElement:   to,
		ToPad:       toPad,
	})
}

func (s *PipelineSpec) ElementByName(name string) *ElementSpec {
	for i := range s.Elements {
		if s.Elements[i].Name == name {
			return &s.Elements[i]
		}
	}
	return nil
}

func (s *PipelineSpec) HasElement(name string) bool {
	return s.ElementByName(name) != nil
}

type SessionOpts struct {
	SourceURL    string
	IsLive       bool
	IsFileSource bool

	VideoCodec    string
	AudioCodec    string
	ContainerHint string

	NeedsTranscode bool
	HWAccel        string
	DecodeHWAccel  string
	OutputCodec    string
	Bitrate        int
	OutputHeight   int

	AudioChannels int
	AudioLanguage string
	AudioPassthrough bool

	SegmentDurationMs uint

	OutputDir      string
	MuxOutputPath  string
}

func OutputDirSegments(outputDir string) string {
	return filepath.Join(outputDir, "segments")
}
