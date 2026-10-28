package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTracePreservesBurstSpacingAndKeyReuse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "trace.json")
	if err := os.WriteFile(p, []byte(`{"duration_ms":1000,"requests":[{"at_ms":10,"key":0,"kind":"page"},{"at_ms":10,"key":0,"kind":"page"},{"at_ms":900,"key":1,"kind":"search"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	keys := []string{"/v0.1/servers?cursor=a&limit=100", "/v0/servers?search=private&limit=5"}
	trace, err := loadReadTrace(p, keys, time.Second, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if trace.count(99) != 3 || trace.offset(0, 99) != 10*time.Millisecond || trace.offset(1, 99) != 10*time.Millisecond || trace.offset(2, 99) != 900*time.Millisecond {
		t.Fatal("arrival spacing changed")
	}
	path, kind := trace.read(2, keys, "unused", "unused")
	if path != keys[1] || kind != "search" {
		t.Fatal("request identity changed")
	}
}
func TestTraceRejectsUnsafePathsAndInvalidOrdering(t *testing.T) {
	for _, path := range []string{"https://example.com/v0/servers", "//example.com/v0/servers", "/v0.1/publish", "/v0/servers?token=private", "/v0/servers?limit=5&limit=100"} {
		if traceReadKind(path) != "" {
			t.Fatalf("accepted unsupported path %q", path)
		}
	}
	p := filepath.Join(t.TempDir(), "trace.json")
	if err := os.WriteFile(p, []byte(`{"duration_ms":1000,"requests":[{"at_ms":20,"key":0,"kind":"page"},{"at_ms":10,"key":0,"kind":"page"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadReadTrace(p, []string{"/v0/servers"}, time.Second, "", ""); err == nil {
		t.Fatal("out of order trace accepted")
	}
	if _, err := loadReadTrace(p, []string{"/v0/servers"}, time.Second, "", "token-file"); err == nil {
		t.Fatal("publishing accepted")
	}
}

func TestTraceCatalogAcceptsDetailReads(t *testing.T) {
	keys := make([]string, 128)
	for i := range keys {
		keys[i] = "/v0.1/servers/io.example%2Fserver/versions/latest"
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "keys.json")
	if err = os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, loaded := loadKeys(p, true)
	if len(loaded) != 128 || traceReadKind(loaded[0]) != "detail" {
		t.Fatal("valid detail catalog rejected")
	}
}
