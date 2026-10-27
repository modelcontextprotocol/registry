package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Calibration must actually exercise the full real-cursor catalog, rather than
// accidentally revisiting a small subset because its length divides the rate.
func TestVariedPagesReachWholeCatalog(t *testing.T) {
	for _, hot := range []int{0, 50, 80, 99} {
		seen := map[int]bool{}
		for n := 0; n < 4000*100; n++ {
			i := pageIndex(n, hot, 4000)
			if i < 0 || i >= 4000 {
				t.Fatalf("invalid index %d", i)
			}
			seen[i] = true
		}
		if len(seen) != 4000 {
			t.Fatalf("hot=%d visited %d/4000 keys", hot, len(seen))
		}
	}
}

func TestExplicitHTTPProtocol(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	for _, h2 := range []bool{false, true} {
		tr := newTransport(h2)
		tr.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr}
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := 1
		if h2 {
			want = 2
		}
		if resp.ProtoMajor != want {
			t.Fatalf("allowHTTP2=%v negotiated %s", h2, resp.Proto)
		}
	}
}
