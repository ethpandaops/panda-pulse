package roll

import (
	"reflect"
	"testing"
)

func testInventory() *inventoryData {
	return &inventoryData{
		Network: "glamsterdam-devnet-4",
		ConsensusClients: []clientInfo{
			{ClientName: "lighthouse-ethrex-1", ClientType: "lighthouse", SSH: "devops@lighthouse-ethrex-1.example.io", BeaconAPI: "bn-lighthouse-ethrex-1.example.io"},
			{ClientName: "lighthouse-nethermind-1", ClientType: "lighthouse", SSH: "devops@lighthouse-nethermind-1.example.io", BeaconAPI: "bn-lighthouse-nethermind-1.example.io"},
			{ClientName: "prysm-ethrex-1", ClientType: "prysm", SSH: "devops@prysm-ethrex-1.example.io", BeaconAPI: "bn-prysm-ethrex-1.example.io"},
		},
		ExecutionClients: []clientInfo{
			{ClientName: "lighthouse-ethrex-1", ClientType: "ethrex", SSH: "devops@lighthouse-ethrex-1.example.io"},
			{ClientName: "lighthouse-nethermind-1", ClientType: "nethermind", SSH: "devops@lighthouse-nethermind-1.example.io"},
			{ClientName: "prysm-ethrex-1", ClientType: "ethrex", SSH: "devops@prysm-ethrex-1.example.io"},
		},
	}
}

func targetNames(ts []Target) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}

	return out
}

func TestResolveTargets(t *testing.T) {
	targets := ResolveTargets(testInventory(), "https")
	if len(targets) != 3 {
		t.Fatalf("want 3 targets, got %d (%v)", len(targets), targetNames(targets))
	}

	var lh Target

	for _, tg := range targets {
		if tg.Name == "lighthouse-ethrex-1" {
			lh = tg
		}
	}

	if lh.BeaconURL != "https://bn-lighthouse-ethrex-1.example.io" {
		t.Errorf("beacon url = %q", lh.BeaconURL)
	}

	got := map[string]bool{}
	for _, tok := range lh.tokens {
		got[tok] = true
	}

	for _, want := range []string{"lighthouse-ethrex-1", "lighthouse", "ethrex", "lighthouse_ethrex"} {
		if !got[want] {
			t.Errorf("missing token %q in %v", want, lh.tokens)
		}
	}
}

func TestSelect(t *testing.T) {
	targets := ResolveTargets(testInventory(), "https")

	cases := []struct {
		expr string
		want []string
	}{
		{"", []string{"lighthouse-ethrex-1", "lighthouse-nethermind-1", "prysm-ethrex-1"}},
		{"all", []string{"lighthouse-ethrex-1", "lighthouse-nethermind-1", "prysm-ethrex-1"}},
		{"lighthouse", []string{"lighthouse-ethrex-1", "lighthouse-nethermind-1"}},
		{"lighthouse_ethrex", []string{"lighthouse-ethrex-1"}},
		{"lighthouse-ethrex-1", []string{"lighthouse-ethrex-1"}},
		{"lighthouse-*", []string{"lighthouse-ethrex-1", "lighthouse-nethermind-1"}},
		{"*-ethrex-1", []string{"lighthouse-ethrex-1", "prysm-ethrex-1"}},
		{"all:!prysm-*", []string{"lighthouse-ethrex-1", "lighthouse-nethermind-1"}},
		{"!prysm-*", []string{"lighthouse-ethrex-1", "lighthouse-nethermind-1"}},
		{"lighthouse-*:!*nethermind*", []string{"lighthouse-ethrex-1"}},
	}

	for _, tc := range cases {
		if got := targetNames(Select(targets, tc.expr)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Select(%q) = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestSuggestionsScope(t *testing.T) {
	targets := ResolveTargets(testInventory(), "https")

	set := map[string]bool{}
	for _, s := range Suggestions(targets, "", "lighthouse", 25) {
		set[s] = true
	}

	for _, want := range []string{"lighthouse", "lighthouse-ethrex-1", "lighthouse_ethrex"} {
		if !set[want] {
			t.Errorf("scope lighthouse missing token %q", want)
		}
	}

	for _, bad := range []string{"prysm", "prysm-ethrex-1", "all"} {
		if set[bad] {
			t.Errorf("scope lighthouse should not include %q", bad)
		}
	}
}
