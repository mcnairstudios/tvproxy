package gstreamer

import "testing"

func TestPipelineSpec_AddElement(t *testing.T) {
	var spec PipelineSpec
	spec.AddElement("src", "tvproxysrc", Props{"location": "http://example.com/stream.ts", "is-live": true})
	spec.AddElement("d", "tvproxydemux", Props{"audio-channels": 2})

	if len(spec.Elements) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(spec.Elements))
	}
	if spec.Elements[0].Name != "src" {
		t.Errorf("element 0 name = %q, want %q", spec.Elements[0].Name, "src")
	}
	if spec.Elements[0].Factory != "tvproxysrc" {
		t.Errorf("element 0 factory = %q, want %q", spec.Elements[0].Factory, "tvproxysrc")
	}
	if spec.Elements[0].Properties["location"] != "http://example.com/stream.ts" {
		t.Errorf("element 0 location = %v, want %q", spec.Elements[0].Properties["location"], "http://example.com/stream.ts")
	}
}

func TestPipelineSpec_Link(t *testing.T) {
	var spec PipelineSpec
	spec.AddElement("a", "fakesrc", nil)
	spec.AddElement("b", "fakesink", nil)
	spec.Link("a", "b")

	if len(spec.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(spec.Links))
	}
	link := spec.Links[0]
	if link.FromElement != "a" || link.ToElement != "b" {
		t.Errorf("link = %s→%s, want a→b", link.FromElement, link.ToElement)
	}
	if link.FromPad != "" || link.ToPad != "" {
		t.Errorf("default link should have empty pad names")
	}
}

func TestPipelineSpec_LinkPads(t *testing.T) {
	var spec PipelineSpec
	spec.AddElement("d", "tvproxydemux", nil)
	spec.AddElement("fmp4", "tvproxyfmp4", nil)
	spec.LinkPads("d", "video", "fmp4", "video")

	if len(spec.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(spec.Links))
	}
	link := spec.Links[0]
	if link.FromPad != "video" || link.ToPad != "video" {
		t.Errorf("pad link = %s.%s→%s.%s, want d.video→fmp4.video",
			link.FromElement, link.FromPad, link.ToElement, link.ToPad)
	}
}

func TestPipelineSpec_ElementByName(t *testing.T) {
	var spec PipelineSpec
	spec.AddElement("src", "tvproxysrc", Props{"location": "http://test"})
	spec.AddElement("d", "tvproxydemux", nil)

	el := spec.ElementByName("src")
	if el == nil {
		t.Fatal("expected to find element 'src'")
	}
	if el.Factory != "tvproxysrc" {
		t.Errorf("factory = %q, want %q", el.Factory, "tvproxysrc")
	}

	if spec.ElementByName("missing") != nil {
		t.Error("expected nil for missing element")
	}
}

func TestPipelineSpec_HasElement(t *testing.T) {
	var spec PipelineSpec
	spec.AddElement("src", "tvproxysrc", nil)

	if !spec.HasElement("src") {
		t.Error("expected HasElement('src') = true")
	}
	if spec.HasElement("missing") {
		t.Error("expected HasElement('missing') = false")
	}
}

func TestSessionOpts_OutputDirSegments(t *testing.T) {
	got := OutputDirSegments("/record/stream1/uuid1")
	want := "/record/stream1/uuid1/segments"
	if got != want {
		t.Errorf("OutputDirSegments = %q, want %q", got, want)
	}
}
