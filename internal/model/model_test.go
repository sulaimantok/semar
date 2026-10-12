package model

import (
	"testing"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

func TestValidateID(t *testing.T) {
	for _, ok := range []string{"a", "os-jkt", "0", "pve-lab-2"} {
		if err := ValidateID("x", ok); err != nil {
			t.Errorf("ValidateID(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-a", "A", "a_b", "a b", string(make([]byte, 64))} {
		if err := ValidateID("x", bad); err == nil {
			t.Errorf("ValidateID(%q) accepted", bad)
		}
	}
}

func TestProviderFingerprintIsNormalised(t *testing.T) {
	a, err := ProviderFingerprint(contract.ProviderProxmox, []string{"https://PVE2.lab:8006/", "https://pve1.lab:8006"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ProviderFingerprint(contract.ProviderProxmox, []string{"https://pve1.lab:8006", "https://pve2.lab:8006", "https://pve1.lab:8006/"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a != "proxmox://pve1.lab:8006,pve2.lab:8006#" {
		t.Fatalf("fingerprints differ or unexpected: %q vs %q", a, b)
	}

	region1, _ := ProviderFingerprint(contract.ProviderOpenStack, []string{"https://keystone.jkt:5000/v3"}, "RegionOne")
	region2, _ := ProviderFingerprint(contract.ProviderOpenStack, []string{"https://keystone.jkt:5000/v3"}, "RegionTwo")
	if region1 == region2 {
		t.Fatal("regions of one Keystone must be different providers")
	}
}

func TestProviderFingerprintRejectsBadEndpoints(t *testing.T) {
	for _, eps := range [][]string{nil, {""}, {"keystone.jkt:5000"}, {"ftp://x"}, {"https://"}} {
		if _, err := ProviderFingerprint(contract.ProviderOpenStack, eps, ""); err == nil {
			t.Errorf("accepted endpoints %q", eps)
		}
	}
}
