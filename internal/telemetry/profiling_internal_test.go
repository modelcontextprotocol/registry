package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProfilingRoutesAreExplicitAndBounded(t *testing.T) {
	mux := profilingMux()
	for _, path := range []string{"/debug/pprof/profile", "/debug/pprof/profile?seconds=0", "/debug/pprof/profile?seconds=61"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
	for _, path := range []string{"/debug/pprof/cmdline", "/debug/pprof/symbol", "/debug/pprof/trace", "/"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNotFound, w.Code)
	}
	stop, err := StartProfiling(false)
	require.NoError(t, err)
	stop()
}
