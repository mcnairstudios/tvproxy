package gstreamer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/rs/zerolog"
)

type RunResult struct {
	ExitReason string
	Err        error
}

type Executor struct {
	log zerolog.Logger
}

func NewExecutor(log zerolog.Logger) *Executor {
	return &Executor{log: log.With().Str("component", "executor").Logger()}
}

func (e *Executor) Run(ctx context.Context, spec PipelineSpec) (*gst.Pipeline, RunResult) {
	pipeline, err := e.Build(spec)
	if err != nil {
		return nil, RunResult{ExitReason: "build_failed", Err: err}
	}

	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		pipeline.SetState(gst.StateNull)
		return nil, RunResult{ExitReason: "state_failed", Err: fmt.Errorf("SetState PLAYING: %w", err)}
	}

	return pipeline, RunResult{}
}

func (e *Executor) Build(spec PipelineSpec) (*gst.Pipeline, error) {
	pipeline, err := gst.NewPipeline("tvproxy")
	if err != nil {
		return nil, fmt.Errorf("gst.NewPipeline: %w", err)
	}

	elements := make(map[string]*gst.Element, len(spec.Elements))

	for _, el := range spec.Elements {
		elem, err := gst.NewElement(el.Factory)
		if err != nil || elem == nil {
			pipeline.SetState(gst.StateNull)
			return nil, fmt.Errorf("element %q (factory %q) not available", el.Name, el.Factory)
		}
		elem.Set("name", el.Name)
		for k, v := range el.Properties {
			if err := elem.SetProperty(k, v); err != nil {
				e.log.Warn().Str("element", el.Name).Str("property", k).Err(err).Msg("failed to set property")
			}
		}
		pipeline.Add(elem)
		elements[el.Name] = elem
	}

	for _, link := range spec.Links {
		from, ok := elements[link.FromElement]
		if !ok {
			pipeline.SetState(gst.StateNull)
			return nil, fmt.Errorf("link: unknown element %q", link.FromElement)
		}
		to, ok := elements[link.ToElement]
		if !ok {
			pipeline.SetState(gst.StateNull)
			return nil, fmt.Errorf("link: unknown element %q", link.ToElement)
		}

		if link.FromPad != "" || link.ToPad != "" {
			if err := linkPads(from, link.FromPad, to, link.ToPad); err != nil {
				pipeline.SetState(gst.StateNull)
				return nil, fmt.Errorf("link pads %s.%s → %s.%s: %w",
					link.FromElement, link.FromPad, link.ToElement, link.ToPad, err)
			}
		} else {
			from.Link(to)
		}
	}

	return pipeline, nil
}

func linkPads(from *gst.Element, fromPadName string, to *gst.Element, toPadName string) error {
	fromPad := from.GetStaticPad(fromPadName)
	if fromPad == nil {
		fromPad = from.GetRequestPad(fromPadName)
	}
	if fromPad == nil {
		return fmt.Errorf("pad %q not found on source element", fromPadName)
	}

	toPad := to.GetStaticPad(toPadName)
	if toPad == nil {
		toPad = to.GetRequestPad(toPadName)
	}
	if toPad == nil {
		return fmt.Errorf("pad %q not found on sink element", toPadName)
	}

	ret := fromPad.Link(toPad)
	if ret != gst.PadLinkOK {
		return fmt.Errorf("pad link returned %d", ret)
	}
	return nil
}

func (e *Executor) ConnectDynamicPads(pipeline *gst.Pipeline, demuxName string, links map[string]string) {
	demux, err := pipeline.GetElementByName(demuxName)
	if err != nil || demux == nil {
		e.log.Error().Str("demux", demuxName).Msg("demux element not found for dynamic pad connection")
		return
	}

	onces := make(map[string]*sync.Once, len(links))
	for prefix := range links {
		onces[prefix] = &sync.Once{}
	}

	demux.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
		caps := pad.GetCurrentCaps()
		padName := pad.GetName()
		capsName := ""
		if caps != nil {
			capsName = caps.GetStructureAt(0).Name()
		}

		for prefix, sinkElementName := range links {
			once, ok := onces[prefix]
			if !ok {
				continue
			}
			isMatch := strings.HasPrefix(capsName, prefix) || strings.HasPrefix(padName, prefix)
			if !isMatch {
				continue
			}
			once.Do(func() {
				sink, sinkErr := pipeline.GetElementByName(sinkElementName)
				if sinkErr != nil || sink == nil {
					return
				}
				sinkPad := sink.GetStaticPad("sink")
				if sinkPad != nil && !sinkPad.IsLinked() {
					pad.Link(sinkPad)
				}
			})
			return
		}

		drainUnlinkedPad(pipeline, pad)
	})
}

func (e *Executor) RunBusLoop(ctx context.Context, pipeline *gst.Pipeline, isLive bool) RunResult {
	bus := pipeline.GetBus()
	lastActivity := time.Now()
	const watchdogTimeout = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return RunResult{ExitReason: "cancelled"}
		}
		msg := bus.TimedPop(gst.ClockTime(500000000))
		if ctx.Err() != nil {
			return RunResult{ExitReason: "cancelled"}
		}
		if msg == nil {
			if isLive && time.Since(lastActivity) > watchdogTimeout {
				return RunResult{ExitReason: "watchdog"}
			}
			continue
		}
		lastActivity = time.Now()
		switch msg.Type() {
		case gst.MessageEOS:
			if isLive {
				return RunResult{ExitReason: "eos_live"}
			}
			return RunResult{ExitReason: "vod_complete"}
		case gst.MessageError:
			gstErr := msg.ParseError()
			errStr := gstErr.Error()
			if strings.Contains(errStr, "Could not multiplex") || strings.Contains(errStr, "clock problem") {
				continue
			}
			return RunResult{ExitReason: "error", Err: gstErr}
		case gst.MessageBuffering:
			pct := msg.ParseBuffering()
			if pct >= 100 {
				pipeline.SetState(gst.StatePlaying)
			}
		}
	}
}

func (e *Executor) Stop(pipeline *gst.Pipeline) {
	if pipeline != nil {
		pipeline.SetState(gst.StateNull)
	}
}

func drainUnlinkedPad(pipeline *gst.Pipeline, pad *gst.Pad) {
	fake, _ := gst.NewElement("fakesink")
	if fake == nil {
		return
	}
	fake.SetProperty("sync", false)
	fake.SetProperty("async", false)
	pipeline.Add(fake)
	fake.SetState(gst.StatePlaying)
	pad.Link(fake.GetStaticPad("sink"))
}
