package certification

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateEnvFile_AppendAndReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker", ".env")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("FOO=bar\n# comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateEnvFile(path, "CERT-001"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "FOO=bar") {
		t.Fatalf("missing FOO: %s", s)
	}
	if !strings.Contains(s, "ORVD_CERTIFICATE_ID=CERT-001") {
		t.Fatalf("missing cert id: %s", s)
	}
	if err := UpdateEnvFile(path, "CERT-002"); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(path)
	s = string(body)
	if strings.Contains(s, "CERT-001") {
		t.Fatalf("old cert id should be replaced: %s", s)
	}
	if !strings.Contains(s, "ORVD_CERTIFICATE_ID=CERT-002") {
		t.Fatalf("missing new cert id: %s", s)
	}
}
