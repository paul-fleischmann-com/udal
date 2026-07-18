package httpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

func deviceWithEndpoint(url string) api.Device {
	return api.Device{ID: "dev-1", Labels: map[string]string{LabelEndpoint: url}}
}

func TestReadProperty_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/properties/temperature" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(wireValue{Float: floatPtr(21.5)})
	}))
	defer srv.Close()

	a := New(nil)
	v, err := a.ReadProperty(context.Background(), deviceWithEndpoint(srv.URL), "temperature")
	if err != nil {
		t.Fatalf("ReadProperty: %v", err)
	}
	if v.FloatVal == nil || *v.FloatVal != 21.5 {
		t.Errorf("ReadProperty value = %+v, want float 21.5", v)
	}
}

func TestReadProperty_MissingEndpointLabel(t *testing.T) {
	a := New(nil)
	_, err := a.ReadProperty(context.Background(), api.Device{ID: "dev-1"}, "temperature")
	if err == nil {
		t.Fatal("expected error for device with no http.endpoint label, got nil")
	}
}

func TestReadProperty_NonOKStatusBecomesStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	a := New(nil)
	_, err := a.ReadProperty(context.Background(), deviceWithEndpoint(srv.URL), "temperature")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("error is not a *StatusError: %v", err)
	}
	if se.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", se.StatusCode)
	}
}

func TestReadProperty_MalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	a := New(nil)
	_, err := a.ReadProperty(context.Background(), deviceWithEndpoint(srv.URL), "temperature")
	if err == nil {
		t.Fatal("expected an error for a malformed response body, got nil")
	}
}

func TestReadProperty_RequestTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	// srv.Close blocks until outstanding handlers return, so block must be
	// closed (unblocking the handler above) before srv.Close runs — defers
	// are LIFO, so this order matters: registering close(block) after
	// srv.Close would deadlock the whole test binary.
	defer srv.Close()
	defer close(block)

	a := New(nil, WithRequestTimeout(20*time.Millisecond))
	_, err := a.ReadProperty(context.Background(), deviceWithEndpoint(srv.URL), "temperature")
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
}

func floatPtr(f float64) *float64 { return &f }
