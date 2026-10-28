package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"
	"time"
)

// StartProfiling is opt-in and binds only to pod loopback. No profiling routes
// are registered on the application mux or exposed by its Service/Ingress.
func StartProfiling(enabled bool) (func(), error) {
	if !enabled {
		return func() {}, nil
	}
	var config net.ListenConfig
	listener, err := config.Listen(context.Background(), "tcp", "127.0.0.1:6060")
	if err != nil {
		return nil, fmt.Errorf("listen for loopback profiling: %w", err)
	}
	server := &http.Server{Handler: profilingMux(), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 70 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Profiling server stopped: %v", err)
		}
	}()
	return func() { _ = server.Close() }, nil
}

func profilingMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/profile", func(w http.ResponseWriter, r *http.Request) {
		seconds, err := strconv.Atoi(r.URL.Query().Get("seconds"))
		if err != nil || seconds < 1 || seconds > 60 {
			http.Error(w, "seconds must be between 1 and 60", http.StatusBadRequest)
			return
		}
		pprof.Profile(w, r)
	})
	mux.Handle("GET /debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("GET /debug/pprof/heap", pprof.Handler("heap"))
	return mux
}
