package fake

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/contracttest"
)

func TestReadOnlyContract(t *testing.T) {
	contracttest.RunReadOnly(t, func(t *testing.T) contract.CloudDriver {
		return New(Config{})
	})
}

func TestReadOnlyContractLargeInventory(t *testing.T) {
	contracttest.RunReadOnly(t, func(t *testing.T) contract.CloudDriver {
		return New(Config{Seed: 42, Hypervisors: 10, VMsPerHypervisor: 100})
	})
}

func TestSameSeedSameInventory(t *testing.T) {
	ctx := context.Background()
	a, err := New(Config{Seed: 7}).ListVMs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(Config{Seed: 7}).ListVMs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("inventories from the same seed differ")
	}
}

func TestFailNext(t *testing.T) {
	d := New(Config{})
	d.SetFaults(Faults{FailNext: 2})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		_, err := d.ListVMs(ctx)
		if !errors.Is(err, contract.ErrUnavailable) || !contract.IsRetryable(err) {
			t.Fatalf("call %d: got %v, want retryable ErrUnavailable", i, err)
		}
	}
	if _, err := d.ListVMs(ctx); err != nil {
		t.Fatalf("third call: %v", err)
	}
}

func TestErrorRateOneAlwaysFails(t *testing.T) {
	d := New(Config{Faults: Faults{ErrorRate: 1}})
	for i := 0; i < 5; i++ {
		if _, err := d.ListHypervisors(context.Background()); !errors.Is(err, contract.ErrUnavailable) {
			t.Fatalf("call %d: got %v, want ErrUnavailable", i, err)
		}
	}
}

func TestLatencyRespectsDeadline(t *testing.T) {
	d := New(Config{Faults: Faults{Latency: time.Second}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := d.ListVMs(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("returned after %s, latency not interrupted", elapsed)
	}
}

func TestDriftIsVisible(t *testing.T) {
	ctx := context.Background()
	d := New(Config{})
	vms, err := d.ListVMs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	since := time.Now()
	changed := vms[0]
	changed.Name = "renamed-outside-semar"
	d.UpsertVM(changed)

	got, err := d.ListVMsChangedSince(ctx, since.Add(-time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "renamed-outside-semar" {
		t.Fatalf("ListVMsChangedSince = %+v, want only the renamed VM", got)
	}

	if !d.DeleteVM(vms[1].ExternalID) {
		t.Fatal("DeleteVM reported missing VM")
	}
	after, err := d.ListVMs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(vms)-1 {
		t.Fatalf("after delete: %d VMs, want %d", len(after), len(vms)-1)
	}
}

func TestConcurrentUse(t *testing.T) {
	d := New(Config{Faults: Faults{ErrorRate: 0.2}})
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = d.ListVMs(ctx)
				d.UpsertVM(contract.VirtualMachine{ExternalID: "concurrent", Name: "c", Status: contract.VMRunning, Arch: contract.ArchAMD64})
			}
		}(i)
	}
	wg.Wait()
}
