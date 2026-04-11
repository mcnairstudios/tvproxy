package ffmpeg

import "github.com/gavinmcnair/tvproxy/pkg/media"

type ProbeResult = media.ProbeResult
type VideoInfo = media.VideoInfo
type AudioTrack = media.AudioTrack
type StreamDisposition = media.StreamDisposition

var StreamHash = media.StreamHash
var IsHTTPURL = media.IsHTTPURL
var IsRTSPURL = media.IsRTSPURL
var ShellSplit = media.ShellSplit
var SanitizeFilename = media.SanitizeFilename
var IsFFmpegNoise = media.IsFFmpegNoise
var MapEncoderHW = media.MapEncoderHW
var NormalizeVideoCodec = media.NormalizeVideoCodec
var NormalizeContainer = media.NormalizeContainer
var DefaultContainer = media.DefaultContainer
var CaptureTPSHeader = media.CaptureTPSHeader
