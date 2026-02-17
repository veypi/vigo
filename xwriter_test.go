package vigo

import (
	"embed"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

//go:embed go.mod
var testEmbedFS embed.FS

func TestXWriter_JSON(t *testing.T) {
	x, _ := createTestX("GET", "/", nil)
	defer release(x)

	data := map[string]string{"msg": "hello"}
	err := x.JSON(data)
	if err != nil {
		t.Fatalf("JSON failed: %v", err)
	}

	resp := x.writer.(*httptest.ResponseRecorder)
	if resp.Code != 200 { // default status
		t.Errorf("Expected 200, got %d", resp.Code)
	}
	if resp.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Expected application/json, got %s", resp.Header().Get("Content-Type"))
	}

	var res map[string]string
	json.Unmarshal(resp.Body.Bytes(), &res)
	if res["msg"] != "hello" {
		t.Errorf("Expected msg=hello, got %s", res["msg"])
	}
}

func TestXWriter_File(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "testfile-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	content := "Hello World"
	tmpFile.WriteString(content)
	tmpFile.Close()

	x, _ := createTestX("GET", "/", nil)
	defer release(x)

	err = x.File(tmpFile.Name())
	if err != nil {
		t.Fatalf("File failed: %v", err)
	}

	resp := x.writer.(*httptest.ResponseRecorder)
	if resp.Code != 200 {
		t.Errorf("Expected 200, got %d", resp.Code)
	}

	// http.ServeContent sets Content-Type based on extension or sniffing
	// .txt -> text/plain
	ct := resp.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Expected text/plain, got %s", ct)
	}

	if resp.Body.String() != content {
		t.Errorf("Expected content '%s', got '%s'", content, resp.Body.String())
	}
}

func TestXWriter_Embed(t *testing.T) {
	x, _ := createTestX("GET", "/", nil)
	defer release(x)

	// go.mod is embedded
	err := x.Embed(&testEmbedFS, "go.mod")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	resp := x.writer.(*httptest.ResponseRecorder)
	if resp.Code != 200 {
		t.Errorf("Expected 200, got %d", resp.Code)
	}

	if resp.Body.Len() == 0 {
		t.Error("Expected body content, got empty")
	}
}
