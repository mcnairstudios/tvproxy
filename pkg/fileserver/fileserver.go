package fileserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

type FileServer struct {
	paths    map[string]string
	listener net.Listener
	srv      *http.Server
	log      zerolog.Logger
	mu       sync.RWMutex
}

func New(log zerolog.Logger) *FileServer {
	return &FileServer{
		paths: make(map[string]string),
		log:   log.With().Str("component", "fileserver").Logger(),
	}
}

func (fs *FileServer) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("fileserver listen: %w", err)
	}
	fs.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/file/", fs.handleFile)

	fs.srv = &http.Server{Handler: mux}
	go fs.srv.Serve(ln)

	fs.log.Info().Int("port", fs.Port()).Msg("internal file server started on 127.0.0.1")
	return nil
}

func (fs *FileServer) Stop() {
	if fs.srv != nil {
		fs.srv.Close()
	}
}

func (fs *FileServer) Port() int {
	if fs.listener == nil {
		return 0
	}
	return fs.listener.Addr().(*net.TCPAddr).Port
}

func (fs *FileServer) Register(filePath string) string {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		abs = filePath
	}

	token := hashPath(abs)

	fs.mu.Lock()
	fs.paths[token] = abs
	fs.mu.Unlock()

	return fmt.Sprintf("http://127.0.0.1:%d/file/%s", fs.Port(), token)
}

func (fs *FileServer) handleFile(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/file/")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	fs.mu.RLock()
	filePath, ok := fs.paths[token]
	fs.mu.RUnlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "cannot open file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	http.ServeContent(w, r, filepath.Base(filePath), info.ModTime(), f)
}

func hashPath(path string) string {
	h := sha256.Sum256([]byte(path))
	return hex.EncodeToString(h[:16])
}
