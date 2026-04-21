package fileserver

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileServer_ServeFile(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.ts")
	require.NoError(t, os.WriteFile(testFile, []byte("hello world"), 0644))

	log := zerolog.Nop()
	fs := New(log)
	require.NoError(t, fs.Start())
	defer fs.Stop()

	assert.Greater(t, fs.Port(), 0)

	url := fs.Register(testFile)
	assert.Contains(t, url, "http://127.0.0.1:")
	assert.Contains(t, url, "/file/")

	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "hello world", string(body))
}

func TestFileServer_RangeRequest(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.ts")
	require.NoError(t, os.WriteFile(testFile, []byte("0123456789"), 0644))

	log := zerolog.Nop()
	fs := New(log)
	require.NoError(t, fs.Start())
	defer fs.Stop()

	url := fs.Register(testFile)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Range", "bytes=3-6")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusPartialContent, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "3456", string(body))
}

func TestFileServer_NotFound(t *testing.T) {
	log := zerolog.Nop()
	fs := New(log)
	require.NoError(t, fs.Start())
	defer fs.Stop()

	resp, err := http.Get("http://127.0.0.1:" + itoa(fs.Port()) + "/file/nonexistent")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestFileServer_RegisterSameFileTwice(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.ts")
	require.NoError(t, os.WriteFile(testFile, []byte("data"), 0644))

	log := zerolog.Nop()
	fs := New(log)
	require.NoError(t, fs.Start())
	defer fs.Stop()

	url1 := fs.Register(testFile)
	url2 := fs.Register(testFile)
	assert.Equal(t, url1, url2)
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
