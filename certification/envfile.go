package certification

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const envKeyORVDCertificateID = "ORVD_CERTIFICATE_ID"

// UpdateEnvFile sets or replaces ORVD_CERTIFICATE_ID in a dotenv-style file.
func UpdateEnvFile(path, certificateID string) error {
	certificateID = strings.TrimSpace(certificateID)
	if certificateID == "" {
		return fmt.Errorf("certificate id is empty")
	}
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		lines = splitEnvLines(data)
	} else if !os.IsNotExist(err) {
		return err
	}
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == envKeyORVDCertificateID {
			lines[i] = envKeyORVDCertificateID + "=" + certificateID
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, envKeyORVDCertificateID+"="+certificateID)
	}
	body := strings.Join(lines, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func splitEnvLines(data []byte) []string {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
