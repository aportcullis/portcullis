package main

import "testing"

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
