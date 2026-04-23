package encode

import (
	"testing"
)

func TestResolveEncoderName_Table(t *testing.T) {
	tests := []struct {
		codec   string
		hwaccel string
		want    string
	}{
		{"h264", "videotoolbox", "h264_videotoolbox"},
		{"h264", "vaapi", "h264_vaapi"},
		{"h264", "qsv", "h264_qsv"},
		{"h264", "nvenc", "h264_nvenc"},
		{"h264", "none", "libx264"},
		{"h264", "", "libx264"},
		{"h265", "videotoolbox", "hevc_videotoolbox"},
		{"h265", "vaapi", "hevc_vaapi"},
		{"h265", "qsv", "hevc_qsv"},
		{"h265", "nvenc", "hevc_nvenc"},
		{"h265", "none", "libx265"},
		{"av1", "vaapi", "av1_vaapi"},
		{"av1", "qsv", "av1_qsv"},
		{"av1", "nvenc", "av1_nvenc"},
		{"av1", "none", "libsvtav1"},
	}

	for _, tt := range tests {
		name := tt.codec + "/" + tt.hwaccel
		if tt.hwaccel == "" {
			name = tt.codec + "/default"
		}
		t.Run(name, func(t *testing.T) {
			got, err := ResolveEncoderName(EncodeOpts{
				Codec:   tt.codec,
				HWAccel: tt.hwaccel,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveEncoderName_UnsupportedCodec(t *testing.T) {
	_, err := ResolveEncoderName(EncodeOpts{Codec: "vp9", HWAccel: "none"})
	if err == nil {
		t.Fatal("expected error for unsupported codec, got nil")
	}
}

func TestResolveEncoderName_UnsupportedHWAccel(t *testing.T) {
	_, err := ResolveEncoderName(EncodeOpts{Codec: "av1", HWAccel: "videotoolbox"})
	if err == nil {
		t.Fatal("expected error for unsupported av1+videotoolbox, got nil")
	}
}

func TestResolveEncoderName_EncoderNameOverride(t *testing.T) {
	got, err := ResolveEncoderName(EncodeOpts{
		Codec:       "h264",
		HWAccel:     "vaapi",
		EncoderName: "my_custom_encoder",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my_custom_encoder" {
		t.Errorf("got %q, want %q", got, "my_custom_encoder")
	}
}

func TestEncodeOpts_Defaults(t *testing.T) {
	var opts EncodeOpts
	if opts.Codec != "" {
		t.Errorf("expected default Codec empty, got %q", opts.Codec)
	}
	if opts.HWAccel != "" {
		t.Errorf("expected default HWAccel empty, got %q", opts.HWAccel)
	}
	if opts.Bitrate != 0 {
		t.Errorf("expected default Bitrate 0, got %d", opts.Bitrate)
	}
	if opts.KeyframeInterval != 0 {
		t.Errorf("expected default KeyframeInterval 0, got %d", opts.KeyframeInterval)
	}
	if opts.Preset != "" {
		t.Errorf("expected default Preset empty, got %q", opts.Preset)
	}
	if opts.Width != 0 {
		t.Errorf("expected default Width 0, got %d", opts.Width)
	}
	if opts.Height != 0 {
		t.Errorf("expected default Height 0, got %d", opts.Height)
	}
	if opts.EncoderName != "" {
		t.Errorf("expected default EncoderName empty, got %q", opts.EncoderName)
	}
}

func TestClose_Idempotent(t *testing.T) {
	e := &Encoder{}
	e.Close()
	e.Close()
}

func TestExtradata_NilBeforeOpen(t *testing.T) {
	e := &Encoder{}
	data := e.Extradata()
	if data != nil {
		t.Errorf("expected nil extradata before open, got %v", data)
	}
}

func TestSoftwareFallbackTable(t *testing.T) {
	for codec := range encoderTable {
		if _, ok := softwareFallback[codec]; !ok {
			t.Errorf("missing software fallback for codec %q", codec)
		}
	}
}

func TestResolveAudioEncoderName(t *testing.T) {
	tests := []struct {
		codec string
		want  string
	}{
		{"aac", "aac"},
		{"ac3", "ac3"},
		{"eac3", "eac3"},
		{"mp2", "mp2"},
		{"flac", "flac"},
		{"opus", "libopus"},
		{"mp3", "libmp3lame"},
		{"vorbis", "libvorbis"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := ResolveAudioEncoderName(tt.codec)
		if got != tt.want {
			t.Errorf("ResolveAudioEncoderName(%q) = %q, want %q", tt.codec, got, tt.want)
		}
	}
}

func TestNewAudioEncoder_AAC(t *testing.T) {
	enc, err := NewAudioEncoder(AudioEncodeOpts{Codec: "aac", Channels: 2, SampleRate: 48000})
	if err != nil {
		t.Fatalf("NewAudioEncoder(aac): %v", err)
	}
	defer enc.Close()
	if enc.FrameSize() != 1024 {
		t.Errorf("AAC frame size = %d, want 1024", enc.FrameSize())
	}
	if len(enc.Extradata()) == 0 {
		t.Error("AAC encoder produced no extradata")
	}
}

func TestNewAudioEncoder_OpusResolvesName(t *testing.T) {
	enc, err := NewAudioEncoder(AudioEncodeOpts{Codec: "opus", Channels: 2, SampleRate: 48000})
	if err != nil {
		t.Skipf("opus encoder not available: %v", err)
	}
	defer enc.Close()
	if enc.FrameSize() <= 0 {
		t.Errorf("Opus frame size = %d, expected > 0", enc.FrameSize())
	}
}

func TestNewAudioEncoder_EmptyCodec(t *testing.T) {
	_, err := NewAudioEncoder(AudioEncodeOpts{Channels: 2, SampleRate: 48000})
	if err == nil {
		t.Fatal("expected error for empty codec")
	}
}

func TestNewAudioEncoder_UnknownCodec(t *testing.T) {
	_, err := NewAudioEncoder(AudioEncodeOpts{Codec: "nonexistent_codec", Channels: 2, SampleRate: 48000})
	if err == nil {
		t.Fatal("expected error for unknown codec")
	}
}

func TestResolveEncoderName_AllTableEntries(t *testing.T) {
	for codec, hwMap := range encoderTable {
		for hw, expectedName := range hwMap {
			got, err := ResolveEncoderName(EncodeOpts{Codec: codec, HWAccel: hw})
			if err != nil {
				t.Errorf("ResolveEncoderName(%s, %s): unexpected error: %v", codec, hw, err)
				continue
			}
			if got != expectedName {
				t.Errorf("ResolveEncoderName(%s, %s) = %q, want %q", codec, hw, got, expectedName)
			}
		}
	}
}
