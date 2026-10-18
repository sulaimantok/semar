package auditsink

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/sulaimantok/semar/internal/store"
	"github.com/sulaimantok/semar/internal/store/storetest"
)

type receiver struct {
	mu      sync.Mutex
	batches []Batch
	status  int
	secret  string
	badSig  int
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.secret != "" && !Verify(r.secret, body, req.Header.Get(SignatureHeader)) {
		r.badSig++
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.status != 0 {
		w.WriteHeader(r.status)
		return
	}
	var b Batch
	_ = json.Unmarshal(body, &b)
	r.batches = append(r.batches, b)
}

func appendEvents(t *testing.T, st *store.Store, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := st.AppendAudit(context.Background(), store.AuditEvent{
			Actor: "admin", Action: "site.create", Target: "site-a", Outcome: store.AuditSuccess,
			Details: map[string]string{"n": string(rune('a' + i))},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeliveryIsOrderedSignedAndResumable(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	appendEvents(t, st, 3)
	rcv := &receiver{secret: "a-long-shared-secret-for-the-sink"} //nolint:gosec // G101: test fixture
	srv := httptest.NewServer(rcv)
	t.Cleanup(srv.Close)
	s := &Streamer{Store: st, URL: srv.URL, Secret: rcv.secret, Batch: 2}

	if n, err := s.DeliverOnce(ctx); err != nil || n != 2 {
		t.Fatalf("first batch = %d, %v", n, err)
	}
	if n, err := s.DeliverOnce(ctx); err != nil || n != 1 {
		t.Fatalf("second batch = %d, %v", n, err)
	}
	if n, err := s.DeliverOnce(ctx); err != nil || n != 0 {
		t.Fatalf("caught up = %d, %v", n, err)
	}
	all := append(append([]Event{}, rcv.batches[0].Events...), rcv.batches[1].Events...)
	if len(all) != 3 || rcv.badSig != 0 {
		t.Fatalf("received %d events, %d bad signatures", len(all), rcv.badSig)
	}
	for i := 1; i < len(all); i++ {
		if all[i].ID <= all[i-1].ID || all[i].PrevHash != all[i-1].Hash {
			t.Fatalf("event %d does not chain to %d", all[i].ID, all[i-1].ID)
		}
	}

	// A failing receiver does not move the cursor; the event is resent.
	appendEvents(t, st, 1)
	rcv.status = http.StatusServiceUnavailable
	if _, err := s.DeliverOnce(ctx); err == nil {
		t.Fatal("delivery to a failing receiver succeeded")
	}
	rcv.status = 0
	if n, err := s.DeliverOnce(ctx); err != nil || n != 1 || rcv.batches[2].Events[0].ID != all[2].ID+1 {
		t.Fatalf("redelivery = %d, %v", n, err)
	}

	// A wrong secret is rejected by a verifying receiver.
	bad := &Streamer{Store: st, URL: srv.URL, Secret: "wrong", Name: "other"}
	if _, err := bad.DeliverOnce(ctx); err == nil || rcv.badSig != 1 {
		t.Fatalf("wrong secret: %v, bad signatures %d", err, rcv.badSig)
	}
}

func TestOnlyOneReplicaLeads(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	release, ok, err := st.TryLeadership(ctx, "semar.auditsink.webhook")
	if err != nil || !ok {
		t.Fatalf("first leader: %v %v", ok, err)
	}
	if _, ok, err := st.TryLeadership(ctx, "semar.auditsink.webhook"); err != nil || ok {
		t.Fatalf("second leader acquired the lock: %v %v", ok, err)
	}
	release()
	if r, ok, err := st.TryLeadership(ctx, "semar.auditsink.webhook"); err != nil || !ok {
		t.Fatalf("after release: %v %v", ok, err)
	} else {
		r()
	}
}
