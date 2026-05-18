package motors

import "testing"

func TestClampAndToFloat(t *testing.T) {
	if clamp(100, 0, 50) != 50 {
		t.Fatal("clamp max")
	}
	if clamp(-1, 0, 50) != 0 {
		t.Fatal("clamp min")
	}
	if v, ok := toFloat(int32(3)); !ok || v != 3 {
		t.Fatalf("int32 %v %v", v, ok)
	}
	if _, ok := toFloat("x"); ok {
		t.Fatal("string")
	}
}

func TestSanitizeTarget(t *testing.T) {
	got, err := sanitizeTarget(map[string]interface{}{
		"lat": 1.0, "lon": 2.0, "drop": true,
	})
	if err != nil || got["drop"] != true {
		t.Fatalf("%#v %v", got, err)
	}
	if _, err := sanitizeTarget(map[string]interface{}{"lat": "bad"}); err == nil {
		t.Fatal("expected invalid lat")
	}
	if _, err := sanitizeTarget(map[string]interface{}{}); err == nil {
		t.Fatal("expected empty")
	}
}
