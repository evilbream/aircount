package system

import (
	"os"
	"testing"
)

func TestCreateTemp(t *testing.T) {
	tmp, err := GetOrCreateTempFile("tplink-*.json")
	if err != nil {
		t.Error("failed to load file")
	}
	if tmp == "" {
		t.Error("temp file is empty")
	}
	tmp2, err := GetOrCreateTempFile("tplink-*.json")
	if err != nil {
		t.Error("failed to create second file")
	}
	if tmp != tmp2 {
		t.Errorf("%s != %s", tmp, tmp2)
	}
	os.Remove(tmp)
}
