package main

import (
	"slices"
	"testing"
)

func TestPortFromEnvironmentReadsAssignedPorts(t *testing.T) {
	for _, scenario := range []struct {
		value string
		want  int
	}{
		{value: "18080", want: 18080},
		{value: "1", want: 1},
		{value: "65535", want: 65535},
		{value: " 49152 ", want: 49152},
	} {
		t.Setenv("E2E_TEST_PORT", scenario.value)
		got, err := portFromEnvironment("E2E_TEST_PORT")
		if err != nil || got != scenario.want {
			t.Errorf("portFromEnvironment(%q) = %d, %v; want %d", scenario.value, got, err, scenario.want)
		}
	}
}

func TestPortFromEnvironmentRefusesMissingOrInvalidPorts(t *testing.T) {
	for _, value := range []string{"", "0", "65536", "-1", "http"} {
		t.Setenv("E2E_TEST_PORT", value)
		if got, err := portFromEnvironment("E2E_TEST_PORT"); err == nil {
			t.Errorf("portFromEnvironment(%q) = %d, want an error", value, got)
		}
	}
}

func TestStaleContainerIDsSelectsOnlyContainersWithoutALiveOwner(t *testing.T) {
	alive := func(pid int) bool { return pid == 100 || pid == 200 }
	containers := []labelledContainer{
		{id: "live-a", owner: "100"},
		{id: "dead", owner: "300"},
		{id: "live-b", owner: "200"},
		{id: "unowned", owner: ""},
		{id: "garbled", owner: "not-a-pid"},
	}
	want := []string{"dead", "unowned", "garbled"}
	if got := staleContainerIDs(containers, alive); !slices.Equal(got, want) {
		t.Errorf("staleContainerIDs = %v, want %v", got, want)
	}
	if got := staleContainerIDs(nil, alive); len(got) != 0 {
		t.Errorf("staleContainerIDs(nil) = %v, want none", got)
	}
}
