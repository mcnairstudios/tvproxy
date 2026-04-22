package subtitle

import (
	"bytes"
	"testing"
)

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "00:00:00.000"},
		{1500, "00:00:01.500"},
		{3661500, "01:01:01.500"},
		{90000, "00:01:30.000"},
	}
	for _, tt := range tests {
		got := FormatTimestamp(tt.ms)
		if got != tt.want {
			t.Errorf("FormatTimestamp(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestConvertSRT(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello", "Hello"},
		{"<b>Bold</b>", "Bold"},
		{"<i>Italic</i> text", "Italic text"},
		{"<font color=\"#fff\">Colored</font>", "Colored"},
		{"{\\an8}Positioned", "Positioned"},
	}
	for _, tt := range tests {
		got := ConvertSRT(tt.input)
		if got != tt.want {
			t.Errorf("ConvertSRT(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestConvertASS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Plain text", "Plain text"},
		{"{\\an8}Top text", "Top text"},
		{"{\\b1}Bold{\\b0} normal", "Bold normal"},
		{"{\\pos(320,50)}Positioned", "Positioned"},
	}
	for _, tt := range tests {
		got := ConvertASS(tt.input)
		if got != tt.want {
			t.Errorf("ConvertASS(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWriter(t *testing.T) {
	w := NewWriter()
	w.AddCue(1_000_000_000, 3_000_000_000, "Hello")  // 1s-4s
	w.AddCue(5_000_000_000, 3_500_000_000, "World")   // 5s-8.5s

	var buf bytes.Buffer
	w.WriteTo(&buf)

	expected := "WEBVTT\n\n00:00:01.000 --> 00:00:04.000\nHello\n\n00:00:05.000 --> 00:00:08.500\nWorld\n"
	if buf.String() != expected {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), expected)
	}
}

func TestWriterEmpty(t *testing.T) {
	w := NewWriter()
	var buf bytes.Buffer
	n, err := w.WriteTo(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len("WEBVTT\n")) {
		t.Errorf("WriteTo returned n=%d, want %d", n, len("WEBVTT\n"))
	}
	if buf.String() != "WEBVTT\n" {
		t.Errorf("got %q, want %q", buf.String(), "WEBVTT\n")
	}
}

func TestWriterMultilineText(t *testing.T) {
	w := NewWriter()
	w.AddCue(1_000_000_000, 2_000_000_000, "Line one\nLine two")

	var buf bytes.Buffer
	w.WriteTo(&buf)

	expected := "WEBVTT\n\n00:00:01.000 --> 00:00:03.000\nLine one\nLine two\n"
	if buf.String() != expected {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), expected)
	}
}
