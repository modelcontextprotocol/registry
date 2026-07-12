package commands

import (
	"testing"
)

func TestValidatePublishData_ValidASCII(t *testing.T) {
	data := []byte(`{"name": "com.example/test", "description": "A simple test server", "version": "1.0.0"}`)
	if err := validatePublishData(data); err != nil {
		t.Errorf("expected no error for valid ASCII JSON, got: %v", err)
	}
}

func TestValidatePublishData_ValidUTF8(t *testing.T) {
	// Valid UTF-8 with em-dash and accented characters
	data := []byte(`{"name": "com.example/test", "description": "Test server — café résumé", "version": "1.0.0"}`)
	if err := validatePublishData(data); err != nil {
		t.Errorf("expected no error for valid UTF-8 JSON, got: %v", err)
	}
}

func TestValidatePublishData_ValidUnicode(t *testing.T) {
	// Valid UTF-8 with various Unicode (Chinese, Japanese, emoji)
	data := []byte(`{"name": "com.example/test", "description": "测试サーバー 🔧", "version": "1.0.0"}`)
	if err := validatePublishData(data); err != nil {
		t.Errorf("expected no error for valid Unicode JSON, got: %v", err)
	}
}

func TestValidatePublishData_InvalidUTF8(t *testing.T) {
	// Invalid UTF-8 sequence: 0xFF is never valid in UTF-8
	// Use interpreted string literal so \xff is the raw byte 0xFF
	data := []byte("{\"name\": \"com.example/test\", \"description\": \"Test \xff server\", \"version\": \"1.0.0\"}")
	if err := validatePublishData(data); err == nil {
		t.Error("expected error for invalid UTF-8, got nil")
	}
}

func TestValidatePublishData_InvalidUTF8MultiByte(t *testing.T) {
	// Invalid UTF-8: truncated multi-byte sequence (0xE2 without continuation bytes)
	data := []byte("{\"name\": \"test\", \"description\": \"\xe2 server\", \"version\": \"1.0.0\"}")
	if err := validatePublishData(data); err == nil {
		t.Error("expected error for truncated UTF-8, got nil")
	}
}

func TestValidatePublishData_LoneSurrogateJSON(t *testing.T) {
	// Marshaled JSON containing a lone surrogate escape (\udc94).
	// This is the pattern observed on Windows systems with CP-936/GBK codepage.
	data := []byte(`{"name": "com.example/test", "description": "test \udc94 server", "version": "1.0.0"}`)
	if err := validatePublishData(data); err == nil {
		t.Error("expected error for lone surrogate, got nil")
	}
}

func TestValidatePublishData_LoneSurrogateUppercase(t *testing.T) {
	// Same but with uppercase hex: \udc94 vs \uDC94
	data := []byte(`{"name": "com.example/test", "description": "test \uDC94 server", "version": "1.0.0"}`)
	if err := validatePublishData(data); err == nil {
		t.Error("expected error for uppercase lone surrogate, got nil")
	}
}

func TestValidatePublishData_LoneSurrogateEndRange(t *testing.T) {
	// \uddff - another lone surrogate in the surrogate range
	data := []byte(`{"name": "com.example/test", "description": "test \uddff server", "version": "1.0.0"}`)
	if err := validatePublishData(data); err == nil {
		t.Error("expected error for lone surrogate at end of range, got nil")
	}
}

func TestValidatePublishData_ValidEscapeNotSurrogate(t *testing.T) {
	// \udc is 4 chars but these are not \udcXX patterns
	// "\\udc" alone doesn't match - it's just the word "udc" in a valid escape
	// or a string that happens to contain these characters
	data := []byte(`{"name": "more-like-udc", "description": "valid string", "version": "1.0.0"}`)
	if err := validatePublishData(data); err != nil {
		t.Errorf("expected no error for valid string containing 'udc', got: %v", err)
	}
}
