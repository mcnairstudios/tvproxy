package fmp4

type Store interface {
	GetInit() ([]byte, int64)
	GetSegment(gen int64, seq int) ([]byte, int64, bool)
	Close()
	Generation() int64
	SegmentCount() int
	TimestampOffset() float64
	GetTimingDebug() TimingDebug
	IsAudioRejected() bool
	GetAudioCodecString() string
}
