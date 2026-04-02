package store

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
)

const (
	SessionActive    = "active"
	SessionRecording = "recording"
	SessionCompleted = "completed"
)

type SessionMeta struct {
	Status       string    `json:"status"`
	SessionID    string    `json:"session_id"`
	StreamID     string    `json:"stream_id"`
	StreamName   string    `json:"stream_name"`
	StreamURL    string    `json:"stream_url"`
	ChannelID    string    `json:"channel_id"`
	ChannelName  string    `json:"channel_name"`
	ProfileName  string    `json:"profile_name"`
	FileName     string    `json:"file_name"`
	StartedAt    time.Time `json:"started_at"`
	ProgramTitle string    `json:"program_title,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	StopAt       time.Time `json:"stop_at,omitempty"`
	StoppedAt    time.Time `json:"stopped_at,omitempty"`
}

type RecordingMeta struct {
	StreamID     string    `json:"stream_id"`
	StreamName   string    `json:"stream_name"`
	ChannelID    string    `json:"channel_id"`
	ChannelName  string    `json:"channel_name"`
	ProgramTitle string    `json:"program_title"`
	UserID       string    `json:"user_id"`
	StartedAt    time.Time `json:"started_at"`
	StoppedAt    time.Time `json:"stopped_at"`
}

type RecordingEntry struct {
	StreamID string         `json:"stream_id"`
	Filename string         `json:"filename"`
	Size     int64          `json:"size"`
	ModTime  string         `json:"mod_time"`
	Meta     *RecordingMeta `json:"meta,omitempty"`
}

type RecordingStoreImpl struct {
	rootDir string
	log     zerolog.Logger
}

func NewRecordingStore(rootDir string, log zerolog.Logger) *RecordingStoreImpl {
	return &RecordingStoreImpl{
		rootDir: rootDir,
		log:     log.With().Str("store", "recording").Logger(),
	}
}

func (s *RecordingStoreImpl) GetProbe(streamHash string) (*ffmpeg.ProbeResult, error) {
	if err := validatePathComponent(streamHash); err != nil {
		return nil, fmt.Errorf("invalid stream hash: %w", err)
	}
	path := filepath.Join(s.rootDir, streamHash, "probe.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result ffmpeg.ProbeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if !isUsefulProbe(&result) {
		return nil, nil
	}
	return &result, nil
}

func (s *RecordingStoreImpl) SaveProbeByStreamID(streamID string, result *ffmpeg.ProbeResult) error {
	if err := validatePathComponent(streamID); err != nil {
		return fmt.Errorf("invalid stream ID: %w", err)
	}
	dir := filepath.Join(s.rootDir, streamID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "probe.json"), data, 0644)
}

func (s *RecordingStoreImpl) GetProbeByStreamID(streamID string) (*ffmpeg.ProbeResult, error) {
	if err := validatePathComponent(streamID); err != nil {
		return nil, err
	}
	path := filepath.Join(s.rootDir, streamID, "probe.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result ffmpeg.ProbeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *RecordingStoreImpl) SaveProbe(streamHash string, result *ffmpeg.ProbeResult) error {
	if err := validatePathComponent(streamHash); err != nil {
		return fmt.Errorf("invalid stream hash: %w", err)
	}
	if !isUsefulProbe(result) {
		return nil
	}

	dir := filepath.Join(s.rootDir, streamHash)
	path := filepath.Join(dir, "probe.json")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *RecordingStoreImpl) InvalidateProbe(streamHash string) error {
	if err := validatePathComponent(streamHash); err != nil {
		return fmt.Errorf("invalid stream hash: %w", err)
	}
	path := filepath.Join(s.rootDir, streamHash, "probe.json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func isUsefulProbe(result *ffmpeg.ProbeResult) bool {
	if result == nil {
		return false
	}
	return result.Video != nil || len(result.AudioTracks) > 0
}

func (s *RecordingStoreImpl) ActiveDir(streamID string) string {
	return filepath.Join(s.rootDir, streamID, "active")
}

func (s *RecordingStoreImpl) RecordedDir(streamID string) string {
	return filepath.Join(s.rootDir, streamID, "recorded")
}

func (s *RecordingStoreImpl) WriteSessionMeta(streamID string, meta SessionMeta) error {
	if err := validatePathComponent(streamID); err != nil {
		return fmt.Errorf("invalid stream ID: %w", err)
	}
	dir := s.ActiveDir(streamID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating active dir: %w", err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), data, 0644)
}

func (s *RecordingStoreImpl) ReadSessionMeta(streamID string) (*SessionMeta, error) {
	if err := validatePathComponent(streamID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.ActiveDir(streamID), "meta.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var meta SessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *RecordingStoreImpl) RemoveActiveSession(streamID string) error {
	if err := validatePathComponent(streamID); err != nil {
		return err
	}
	return os.RemoveAll(s.ActiveDir(streamID))
}

func (s *RecordingStoreImpl) CompleteRecording(streamID string, meta SessionMeta) (string, error) {
	if err := validatePathComponent(streamID); err != nil {
		return "", fmt.Errorf("invalid stream ID: %w", err)
	}

	activeDir := s.ActiveDir(streamID)
	recordedDir := s.RecordedDir(streamID)
	if err := os.MkdirAll(recordedDir, 0755); err != nil {
		return "", fmt.Errorf("creating recorded dir: %w", err)
	}

	srcPath := filepath.Join(activeDir, meta.FileName)
	baseName := ffmpeg.SanitizeFilename(meta.ProgramTitle, meta.StoppedAt)
	mp4Name := baseName + ".mp4"
	destPath := filepath.Join(recordedDir, mp4Name)

	for i := 1; i <= 1000; i++ {
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			break
		}
		if i == 1000 {
			return "", fmt.Errorf("too many filename collisions for %s", baseName)
		}
		mp4Name = fmt.Sprintf("%s_%d.mp4", baseName, i)
		destPath = filepath.Join(recordedDir, mp4Name)
	}

	if err := moveOrCopy(srcPath, destPath); err != nil {
		return "", fmt.Errorf("moving recording to completed: %w", err)
	}

	meta.Status = SessionCompleted
	meta.FileName = mp4Name
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling metadata: %w", err)
	}
	jsonName := strings.TrimSuffix(mp4Name, ".mp4") + ".json"
	if err := os.WriteFile(filepath.Join(recordedDir, jsonName), metaData, 0644); err != nil {
		s.log.Warn().Err(err).Str("path", jsonName).Msg("failed to write recording metadata")
	}

	os.RemoveAll(activeDir)

	return mp4Name, nil
}

func (s *RecordingStoreImpl) ListActiveRecordings() ([]SessionMeta, error) {
	streamDirs, err := os.ReadDir(s.rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result []SessionMeta
	for _, sd := range streamDirs {
		if !sd.IsDir() {
			continue
		}
		meta, err := s.ReadSessionMeta(sd.Name())
		if err != nil || meta == nil {
			continue
		}
		if meta.Status == SessionRecording {
			result = append(result, *meta)
		}
	}
	return result, nil
}

func (s *RecordingStoreImpl) GetMeta(streamHash, filename string) (*RecordingMeta, error) {
	if err := validatePathComponent(streamHash); err != nil {
		return nil, fmt.Errorf("invalid stream hash: %w", err)
	}
	if err := validatePathComponent(filename); err != nil {
		return nil, fmt.Errorf("invalid filename: %w", err)
	}
	jsonName := strings.TrimSuffix(filename, filepath.Ext(filename)) + ".json"
	for _, subdir := range []string{"recorded", "recording"} {
		data, err := os.ReadFile(filepath.Join(s.rootDir, streamHash, subdir, jsonName))
		if err != nil {
			continue
		}
		meta := s.parseRecordingMeta(data)
		if meta != nil {
			return meta, nil
		}
	}
	return nil, nil
}


func (s *RecordingStoreImpl) List(userID string, isAdmin bool) ([]RecordingEntry, error) {
	streamDirs, err := os.ReadDir(s.rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []RecordingEntry{}, nil
		}
		return nil, err
	}

	var entries []RecordingEntry
	for _, sd := range streamDirs {
		if !sd.IsDir() {
			continue
		}
		streamHash := sd.Name()
		for _, subdir := range []string{"recorded", "recording"} {
			recDir := filepath.Join(s.rootDir, streamHash, subdir)
			s.scanRecordingDir(recDir, streamHash, userID, isAdmin, &entries)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModTime > entries[j].ModTime
	})

	if len(entries) == 0 {
		return []RecordingEntry{}, nil
	}
	return entries, nil
}

func (s *RecordingStoreImpl) scanRecordingDir(recDir, streamHash, userID string, isAdmin bool, entries *[]RecordingEntry) {
	files, err := os.ReadDir(recDir)
	if err != nil {
		return
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".mp4") {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}

		entry := RecordingEntry{
			StreamID: streamHash,
			Filename: f.Name(),
			Size:     info.Size(),
			ModTime:  info.ModTime().Format(time.RFC3339),
		}

		jsonName := strings.TrimSuffix(f.Name(), ".mp4") + ".json"
		jsonPath := filepath.Join(recDir, jsonName)
		if data, err := os.ReadFile(jsonPath); err == nil {
			meta := s.parseRecordingMeta(data)
			if meta != nil {
				entry.Meta = meta
			}
		}

		if !isAdmin && entry.Meta != nil && entry.Meta.UserID != "" && entry.Meta.UserID != userID {
			continue
		}

		*entries = append(*entries, entry)
	}
}

func (s *RecordingStoreImpl) parseRecordingMeta(data []byte) *RecordingMeta {
	var session SessionMeta
	if json.Unmarshal(data, &session) == nil && session.Status == SessionCompleted {
		return &RecordingMeta{
			StreamID:     session.StreamID,
			StreamName:   session.StreamName,
			ChannelID:    session.ChannelID,
			ChannelName:  session.ChannelName,
			ProgramTitle: session.ProgramTitle,
			UserID:       session.UserID,
			StartedAt:    session.StartedAt,
			StoppedAt:    session.StoppedAt,
		}
	}
	var legacy RecordingMeta
	if json.Unmarshal(data, &legacy) == nil && legacy.StreamID != "" {
		return &legacy
	}
	return nil
}

func (s *RecordingStoreImpl) FilePath(streamHash, filename string) (string, error) {
	if err := validatePathComponent(streamHash); err != nil {
		return "", fmt.Errorf("invalid stream hash: %w", err)
	}
	if err := validatePathComponent(filename); err != nil {
		return "", fmt.Errorf("invalid filename: %w", err)
	}
	for _, subdir := range []string{"recorded", "recording"} {
		fullPath := filepath.Join(s.rootDir, streamHash, subdir, filename)
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath, nil
		}
	}
	return "", fmt.Errorf("file not found")
}

func (s *RecordingStoreImpl) Delete(streamHash, filename string) error {
	if err := validatePathComponent(streamHash); err != nil {
		return fmt.Errorf("invalid stream hash: %w", err)
	}
	if err := validatePathComponent(filename); err != nil {
		return fmt.Errorf("invalid filename: %w", err)
	}

	jsonName := strings.TrimSuffix(filename, filepath.Ext(filename)) + ".json"

	for _, subdir := range []string{"recorded", "recording"} {
		recDir := filepath.Join(s.rootDir, streamHash, subdir)
		mp4Path := filepath.Join(recDir, filename)
		if err := os.Remove(mp4Path); err != nil && !os.IsNotExist(err) {
			continue
		}
		os.Remove(filepath.Join(recDir, jsonName))

		remaining, _ := os.ReadDir(recDir)
		if len(remaining) == 0 {
			os.Remove(recDir)
		}
		break
	}

	streamDir := filepath.Join(s.rootDir, streamHash)
	top, _ := os.ReadDir(streamDir)
	probeOnly := len(top) == 0 || (len(top) == 1 && top[0].Name() == "probe.json")
	if probeOnly {
		os.RemoveAll(streamDir)
	}

	return nil
}

func validatePathComponent(s string) error {
	if s == "" || s == "." || s == ".." ||
		strings.Contains(s, "/") || strings.Contains(s, "\\") ||
		strings.Contains(s, "\x00") {
		return fmt.Errorf("invalid path component: %q", s)
	}
	return nil
}

func moveOrCopy(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(dst)
		return err
	}

	srcFile.Close()
	os.Remove(src)
	return nil
}
