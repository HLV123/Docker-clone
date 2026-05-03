package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const SocketPath = "/var/run/mydocker.sock"

// Server is the daemon HTTP server
type Server struct {
	handler ContainerHandler
	server  *http.Server
}

// ContainerHandler defines what the daemon can do
type ContainerHandler interface {
	CreateContainer(req *CreateContainerRequest) (*CreateContainerResponse, error)
	StartContainer(id string, req *StartContainerRequest) error
	StopContainer(id string, timeout int) error
	RemoveContainer(id string, force bool) error
	ListContainers(all bool) ([]*ContainerInfo, error)
	InspectContainer(id string) (*ContainerInfo, error)
	GetLogs(id string) (string, error)
	GetStats(id string) (*StatsResponse, error)
	ExecContainer(id string, req *ExecRequest) error
	PullImage(image string) error
	ListImages() ([]*ImageInfo, error)
	BuildImage(req *BuildRequest) error
}

func NewServer(handler ContainerHandler) *Server {
	s := &Server{handler: handler}
	mux := http.NewServeMux()

	// Container endpoints
	mux.HandleFunc("/containers", s.handleContainers)
	mux.HandleFunc("/containers/", s.handleContainer)

	// Image endpoints
	mux.HandleFunc("/images", s.handleImages)
	mux.HandleFunc("/images/pull", s.handlePull)
	mux.HandleFunc("/build", s.handleBuild)

	// Health check
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "OK")
	})

	s.server = &http.Server{Handler: mux}
	return s
}

func (s *Server) Listen() error {
	// Remove old socket
	_ = os.Remove(SocketPath)
	if err := os.MkdirAll(filepath.Dir(SocketPath), 0755); err != nil {
		return err
	}

	listener, err := net.Listen("unix", SocketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", SocketPath, err)
	}
	_ = os.Chmod(SocketPath, 0666)

	fmt.Printf("mydockerd listening on %s\n", SocketPath)
	return s.server.Serve(listener)
}

func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
}

// --- Route handlers ---

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		all := r.URL.Query().Get("all") == "true"
		containers, err := s.handler.ListContainers(all)
		if err != nil {
			writeError(w, err, 500)
			return
		}
		writeJSON(w, containers, 200)
	case http.MethodPost:
		var req CreateContainerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err, 400)
			return
		}
		resp, err := s.handler.CreateContainer(&req)
		if err != nil {
			writeError(w, err, 500)
			return
		}
		writeJSON(w, resp, 201)
	default:
		w.WriteHeader(405)
	}
}

func (s *Server) handleContainer(w http.ResponseWriter, r *http.Request) {
	// Parse: /containers/{id}[/action]
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/containers/"), "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case r.Method == http.MethodGet && action == "":
		info, err := s.handler.InspectContainer(id)
		if err != nil {
			writeError(w, err, 404)
			return
		}
		writeJSON(w, info, 200)

	case r.Method == http.MethodPost && action == "start":
		var req StartContainerRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if err := s.handler.StartContainer(id, &req); err != nil {
			writeError(w, err, 500)
			return
		}
		w.WriteHeader(204)

	case r.Method == http.MethodPost && action == "stop":
		timeout := 10
		if t := r.URL.Query().Get("timeout"); t != "" {
			fmt.Sscanf(t, "%d", &timeout)
		}
		if err := s.handler.StopContainer(id, timeout); err != nil {
			writeError(w, err, 500)
			return
		}
		w.WriteHeader(204)

	case r.Method == http.MethodDelete:
		force := r.URL.Query().Get("force") == "true"
		if err := s.handler.RemoveContainer(id, force); err != nil {
			writeError(w, err, 500)
			return
		}
		w.WriteHeader(204)

	case r.Method == http.MethodGet && action == "logs":
		logs, err := s.handler.GetLogs(id)
		if err != nil {
			writeError(w, err, 404)
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, logs)

	case r.Method == http.MethodGet && action == "stats":
		stats, err := s.handler.GetStats(id)
		if err != nil {
			writeError(w, err, 404)
			return
		}
		writeJSON(w, stats, 200)

	case r.Method == http.MethodPost && action == "exec":
		var req ExecRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, err, 400)
			return
		}
		if err := s.handler.ExecContainer(id, &req); err != nil {
			writeError(w, err, 500)
			return
		}
		w.WriteHeader(204)

	default:
		w.WriteHeader(404)
	}
}

func (s *Server) handleImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	images, err := s.handler.ListImages()
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, images, 200)
}

func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var req PullRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err, 400)
		return
	}
	if err := s.handler.PullImage(req.Image); err != nil {
		writeError(w, err, 500)
		return
	}
	w.WriteHeader(200)
	fmt.Fprintf(w, "Pulled %s\n", req.Image)
}

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var req BuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err, 400)
		return
	}
	if err := s.handler.BuildImage(&req); err != nil {
		writeError(w, err, 500)
		return
	}
	w.WriteHeader(200)
	fmt.Fprintf(w, "Built %s\n", req.Tag)
}

func writeJSON(w http.ResponseWriter, v interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
}
