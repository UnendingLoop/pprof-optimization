package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCustomTime_RFC3339(t *testing.T) {
	var ct CustomTime
	b := []byte(`"2021-11-26T06:22:19Z"`)
	if err := json.Unmarshal(b, &ct); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if ct.IsZero() || ct.UTC().Format(time.RFC3339) != "2021-11-26T06:22:19Z" {
		t.Fatalf("parse failed: %v", ct)
	}
}

func TestCustomTime_Unix(t *testing.T) {
	var ct CustomTime
	b := []byte(`1637907727`)
	if err := json.Unmarshal(b, &ct); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if ct.UTC().Format(time.RFC3339) != "2021-11-26T06:22:07Z" &&
		ct.UTC().Format(time.RFC3339) != "2021-11-26T06:22:19Z" {
		t.Fatalf("got %v", ct.UTC())
	}
}
