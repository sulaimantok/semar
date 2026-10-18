package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

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
