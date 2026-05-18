package certification

import "testing"

func TestParseFirmwareCertificate_Success(t *testing.T) {
	res := &FirmwareCertResult{
		RequestID: "req-1",
		Status:    "CERTIFIED",
		Certificate: map[string]interface{}{
			"certificate_id":        "CERT-FW-123",
			"digital_signature":     "abc",
			"requirements_checked":  []interface{}{"FW-SEC-01", "FW-SEC-02"},
		},
	}
	cert, err := parseFirmwareCertificate(res)
	if err != nil {
		t.Fatal(err)
	}
	if cert.CertificateID != "CERT-FW-123" {
		t.Fatalf("cert id: %s", cert.CertificateID)
	}
	if len(cert.SecurityGoalsChecked) != 2 {
		t.Fatalf("goals: %v", cert.SecurityGoalsChecked)
	}
}

func TestParseFirmwareCertificate_Rejected(t *testing.T) {
	_, err := parseFirmwareCertificate(&FirmwareCertResult{
		RequestID: "req-2",
		Status:    "REJECTED",
	})
	if err == nil {
		t.Fatal("expected error for REJECTED")
	}
}
