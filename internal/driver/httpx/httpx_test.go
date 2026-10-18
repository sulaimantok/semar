package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

func TestSameOriginRedirectIsFollowed(t *testing.T) {
	var token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2.1" {
			http.Redirect(w, r, "/v2.1/", http.StatusFound) // what Nova does
			return
		}
		token = r.Header.Get("X-Auth-Token")
		_, _ = w.Write([]byte(`{"version":{"version":"2.96"}}`))
	}))
	t.Cleanup(srv.Close)
	c, _ := Client(0, "")
	var out struct {
		Version struct{ Version string } `json:"version"`
	}
	if _, err := Do(context.Background(), c, Request{Method: http.MethodGet, URL: srv.URL + "/v2.1", Header: http.Header{"X-Auth-Token": {"t"}}}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Version.Version != "2.96" || token != "t" {
		t.Fatalf("same-origin redirect: %+v, token %q", out, token)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("X-Auth-Token")
	}))
	t.Cleanup(other.Close)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	}))
	t.Cleanup(api.Close)

	c, err := Client(0, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Do(context.Background(), c, Request{Method: http.MethodGet, URL: api.URL, Header: http.Header{"X-Auth-Token": {"secret-token"}}}, nil)
	if !errors.Is(err, ErrRedirect) || !errors.Is(err, contract.ErrUnavailable) {
		t.Fatalf("redirect: %v", err)
	}
	if leaked != "" {
		t.Fatal("the token was sent to the redirect target")
	}
}

func TestClassify(t *testing.T) {
	for status, want := range map[int]error{401: contract.ErrUnauthorized, 403: contract.ErrUnauthorized, 404: contract.ErrNotFound,
		429: contract.ErrRateLimited, 500: contract.ErrUnavailable, 502: contract.ErrUnavailable} {
		if got := Classify(status); !errors.Is(got, want) {
			t.Errorf("Classify(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestProviderMessage(t *testing.T) {
	for body, want := range map[string]string{
		`{"badRequest": {"message": "Unavailable console type novnc.", "code": 400}}`:               "Unavailable console type novnc.",
		`{"error": {"message": "The request you have made requires authentication.", "code": 401}}`: "The request you have made requires authentication.",
		`{"message": "Permission check failed (/vms/100, VM.Console)\n"}`:                           "Permission check failed (/vms/100, VM.Console)",
		`not json`: "",
	} {
		if got := providerMessage([]byte(body)); got != want {
			t.Errorf("providerMessage(%s) = %q, want %q", body, got, want)
		}
	}
}
