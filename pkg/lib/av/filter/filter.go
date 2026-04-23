package filter

import (
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
)

type Deinterlacer struct {
	graph      *astiav.FilterGraph
	bufferSrc  *astiav.BuffersrcFilterContext
	bufferSink *astiav.BuffersinkFilterContext
}

func NewDeinterlacer(width, height int, pixFmt astiav.PixelFormat, timeBase astiav.Rational) (*Deinterlacer, error) {
	graph := astiav.AllocFilterGraph()
	if graph == nil {
		return nil, errors.New("filter: failed to allocate filter graph")
	}

	buffersrc := astiav.FindFilterByName("buffer")
	if buffersrc == nil {
		graph.Free()
		return nil, errors.New("filter: buffer filter not found")
	}
	buffersink := astiav.FindFilterByName("buffersink")
	if buffersink == nil {
		graph.Free()
		return nil, errors.New("filter: buffersink filter not found")
	}

	srcCtx, err := graph.NewBuffersrcFilterContext(buffersrc, "in")
	if err != nil {
		graph.Free()
		return nil, fmt.Errorf("filter: creating buffersrc: %w", err)
	}
	sinkCtx, err := graph.NewBuffersinkFilterContext(buffersink, "out")
	if err != nil {
		graph.Free()
		return nil, fmt.Errorf("filter: creating buffersink: %w", err)
	}

	params := astiav.AllocBuffersrcFilterContextParameters()
	defer params.Free()
	params.SetWidth(width)
	params.SetHeight(height)
	params.SetPixelFormat(pixFmt)
	params.SetTimeBase(timeBase)
	if err := srcCtx.SetParameters(params); err != nil {
		graph.Free()
		return nil, fmt.Errorf("filter: setting buffersrc params: %w", err)
	}
	if err := srcCtx.Initialize(nil); err != nil {
		graph.Free()
		return nil, fmt.Errorf("filter: initializing buffersrc: %w", err)
	}

	outputs := astiav.AllocFilterInOut()
	if outputs == nil {
		graph.Free()
		return nil, errors.New("filter: failed to allocate filter outputs")
	}
	defer outputs.Free()
	outputs.SetName("in")
	outputs.SetFilterContext(srcCtx.FilterContext())
	outputs.SetPadIdx(0)
	outputs.SetNext(nil)

	inputs := astiav.AllocFilterInOut()
	if inputs == nil {
		graph.Free()
		return nil, errors.New("filter: failed to allocate filter inputs")
	}
	defer inputs.Free()
	inputs.SetName("out")
	inputs.SetFilterContext(sinkCtx.FilterContext())
	inputs.SetPadIdx(0)
	inputs.SetNext(nil)

	if err := graph.Parse("yadif=mode=send_frame:parity=auto:deint=interlaced", inputs, outputs); err != nil {
		graph.Free()
		return nil, fmt.Errorf("filter: parsing yadif filter: %w", err)
	}
	if err := graph.Configure(); err != nil {
		graph.Free()
		return nil, fmt.Errorf("filter: configuring graph: %w", err)
	}

	return &Deinterlacer{
		graph:      graph,
		bufferSrc:  srcCtx,
		bufferSink: sinkCtx,
	}, nil
}

func (d *Deinterlacer) Process(frame *astiav.Frame) (*astiav.Frame, error) {
	if err := d.bufferSrc.AddFrame(frame, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
		return nil, fmt.Errorf("filter: adding frame to buffersrc: %w", err)
	}
	out := astiav.AllocFrame()
	if err := d.bufferSink.GetFrame(out, astiav.NewBuffersinkFlags()); err != nil {
		out.Free()
		if errors.Is(err, astiav.ErrEagain) {
			return nil, nil
		}
		return nil, fmt.Errorf("filter: getting frame from buffersink: %w", err)
	}
	return out, nil
}

func (d *Deinterlacer) Close() {
	if d.graph != nil {
		d.graph.Free()
		d.graph = nil
	}
}
