# Phase 1: Foundation — Create plugin packages with interfaces only

## Plans 02-06: No code changes to existing files. Just new packages.

### Plan 02: Create pkg/source/ package
- `pkg/source/source.go` — Source interface, SourceType, SourceInfo
- `pkg/source/types.go` — DiscoveredDevice, RefreshStatus
- `pkg/source/registry.go` — Registry implementation
- `pkg/source/sink.go` — StreamSink interface
- Tests: registry_test.go with mock source
- **Zero changes to existing code**

### Plan 03: Create pkg/output/ package  
- `pkg/output/plugin.go` — OutputPlugin, ServablePlugin, PluginStatus, DeliveryMode
- `pkg/output/config.go` — OutputPluginConfig
- `pkg/output/registry.go` — Registry (factory)
- `pkg/output/fanout.go` — FanOut distributor
- `pkg/output/profile.go` — ClientProfile, ProfileResolver
- Tests: fanout_test.go with mock plugins
- **Zero changes to existing code**

### Plan 04: Create pkg/output/mse/ package
- Extract MSE muxing logic from gopipeline.go MSECopyPipeline
- MSEPlugin implements OutputPlugin + ServablePlugin
- Owns FragmentedMuxer + Watcher
- PushVideo/PushAudio → conv.ToAVPacket → muxer.WriteVideoPacket/WriteAudioPacket
- ServeHTTP serves init segments + media segments
- Tests: basic packet write + segment production
- **Does not delete existing MSECopyPipeline yet**

### Plan 05: Create pkg/output/hls/ package
- Extract HLS muxing logic from gopipeline.go HLSCopyPipeline
- HLSPlugin implements OutputPlugin + ServablePlugin
- Owns HLSMuxer (libavformat native)
- ServeHTTP serves playlist.m3u8 + seg*.ts
- Tests: basic packet write + segment production
- **Does not delete existing HLSCopyPipeline yet**

### Plan 06: Create pkg/output/stream/ package
- Extract stream muxing logic from gopipeline.go StreamCopyPipeline
- StreamPlugin implements OutputPlugin
- Owns StreamMuxer + file handle
- PushVideo/PushAudio write to continuous file (mp4/ts)
- Tests: basic packet write + file production
- **Does not delete existing StreamCopyPipeline yet**
