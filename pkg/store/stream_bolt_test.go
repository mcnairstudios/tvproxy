package store

import (
	"context"
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/rs/zerolog"
)

func TestBoltStreamStore_CRUD(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()
	s, err := NewBoltStreamStore(dir, log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()

	streams := []models.Stream{
		{ID: "s1", M3UAccountID: "acc1", Name: "BBC One", Group: "UK Live", VODType: "", CacheType: "xtream", Language: "EN", IsActive: true},
		{ID: "s2", M3UAccountID: "acc1", Name: "ITV", Group: "UK Live", IsActive: true},
		{ID: "s3", M3UAccountID: "acc2", Name: "CNN", Group: "US News", IsActive: true},
		{ID: "s4", M3UAccountID: "acc1", Name: "Movie A", Group: "Movies", VODType: "movie", IsActive: true},
	}
	if err := s.BulkUpsert(ctx, streams); err != nil {
		t.Fatal(err)
	}

	st, err := s.GetByID(ctx, "s1")
	if err != nil || st == nil {
		t.Fatal("GetByID failed")
	}
	if st.Name != "BBC One" {
		t.Fatalf("expected BBC One, got %s", st.Name)
	}

	byAcc, err := s.ListByAccountID(ctx, "acc1")
	if err != nil || len(byAcc) != 3 {
		t.Fatalf("expected 3 streams for acc1, got %d", len(byAcc))
	}

	byVOD, err := s.ListByVODType(ctx, "movie")
	if err != nil || len(byVOD) != 1 {
		t.Fatalf("expected 1 movie, got %d", len(byVOD))
	}

	all, _ := s.List(ctx)
	if len(all) != 4 {
		t.Fatalf("expected 4 total, got %d", len(all))
	}

	summaries, _ := s.ListSummaries(ctx)
	if len(summaries) != 4 {
		t.Fatalf("expected 4 summaries, got %d", len(summaries))
	}
}

func TestBoltStreamStore_PreservesFields(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()
	s, err := NewBoltStreamStore(dir, log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()

	s.BulkUpsert(ctx, []models.Stream{
		{ID: "s1", M3UAccountID: "acc1", Name: "Test", CacheType: "xtream", Language: "EN", VODType: "movie", TMDBID: 603},
	})

	s.BulkUpsert(ctx, []models.Stream{
		{ID: "s1", M3UAccountID: "acc1", Name: "Test"},
	})

	st, _ := s.GetByID(ctx, "s1")
	if st.CacheType != "xtream" {
		t.Fatalf("CacheType not preserved: %q", st.CacheType)
	}
	if st.Language != "EN" {
		t.Fatalf("Language not preserved: %q", st.Language)
	}
	if st.VODType != "movie" {
		t.Fatalf("VODType not preserved: %q", st.VODType)
	}
	if st.TMDBID != 603 {
		t.Fatalf("TMDBID not preserved: %d", st.TMDBID)
	}
}

func TestBoltStreamStore_DeleteStale(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()
	s, err := NewBoltStreamStore(dir, log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()

	s.BulkUpsert(ctx, []models.Stream{
		{ID: "s1", M3UAccountID: "acc1", Name: "Keep", Group: "G1"},
		{ID: "s2", M3UAccountID: "acc1", Name: "Delete", Group: "G1"},
		{ID: "s3", M3UAccountID: "acc1", Name: "Also Delete", Group: "G2"},
		{ID: "s4", M3UAccountID: "acc2", Name: "Other Account", Group: "G1"},
	})

	deleted, _ := s.DeleteStaleByAccountID(ctx, "acc1", []string{"s1"})
	if len(deleted) != 2 {
		t.Fatalf("expected 2 deleted, got %d", len(deleted))
	}

	byAcc, _ := s.ListByAccountID(ctx, "acc1")
	if len(byAcc) != 1 || byAcc[0].ID != "s1" {
		t.Fatalf("expected only s1, got %v", byAcc)
	}

	byAcc2, _ := s.ListByAccountID(ctx, "acc2")
	if len(byAcc2) != 1 {
		t.Fatalf("acc2 should be untouched, got %d", len(byAcc2))
	}
}

func TestBoltStreamStore_ETag(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()
	s, err := NewBoltStreamStore(dir, log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()

	etag1 := s.ETag()
	s.BulkUpsert(ctx, []models.Stream{{ID: "s1", Name: "Test"}})
	etag2 := s.ETag()

	if etag1 == etag2 {
		t.Fatal("ETag should change after write")
	}
}

func TestBoltStreamStore_UpdateStreamProbeData(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()
	s, err := NewBoltStreamStore(dir, log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()

	s.BulkUpsert(ctx, []models.Stream{
		{ID: "s1", M3UAccountID: "acc1", Name: "Movie A", VODDuration: 0, VODVCodec: "", VODACodec: ""},
	})

	if err := s.UpdateStreamProbeData(ctx, "s1", 120.5, "hevc", "aac"); err != nil {
		t.Fatalf("UpdateStreamProbeData failed: %v", err)
	}

	st, err := s.GetByID(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if st.VODDuration != 120.5 {
		t.Fatalf("expected VODDuration=120.5, got %f", st.VODDuration)
	}
	if st.VODVCodec != "hevc" {
		t.Fatalf("expected VODVCodec=hevc, got %q", st.VODVCodec)
	}
	if st.VODACodec != "aac" {
		t.Fatalf("expected VODACodec=aac, got %q", st.VODACodec)
	}

	if err := s.UpdateStreamProbeData(ctx, "s1", 999.0, "h264", "mp3"); err != nil {
		t.Fatalf("second UpdateStreamProbeData failed: %v", err)
	}

	st, err = s.GetByID(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if st.VODDuration != 120.5 {
		t.Fatalf("VODDuration should not be overwritten, got %f", st.VODDuration)
	}
	if st.VODVCodec != "hevc" {
		t.Fatalf("VODVCodec should not be overwritten, got %q", st.VODVCodec)
	}
	if st.VODACodec != "aac" {
		t.Fatalf("VODACodec should not be overwritten, got %q", st.VODACodec)
	}
}

func TestBoltStreamStore_UpdateStreamProbeData_NotFound(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()
	s, err := NewBoltStreamStore(dir, log)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()

	if err := s.UpdateStreamProbeData(ctx, "nonexistent", 120.5, "hevc", "aac"); err == nil {
		t.Fatal("expected error for nonexistent stream")
	}
}
