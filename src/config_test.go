package main

import "testing"

func TestReadPositiveEnvInt(t *testing.T) {
	const name = "TEST_POSITIVE_ENV_INT"
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{name: "unset", value: "", want: 32},
		{name: "valid", value: "64", want: 64},
		{name: "zero", value: "0", want: 32},
		{name: "negative", value: "-1", want: 32},
		{name: "invalid", value: "many", want: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(name, tt.value)
			if got := readPositiveEnvInt(name, 32); got != tt.want {
				t.Fatalf("readPositiveEnvInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadPositiveEnvInt32(t *testing.T) {
	const name = "TEST_POSITIVE_ENV_INT32"
	tests := []struct {
		name  string
		value string
		want  int32
	}{
		{name: "valid", value: "128", want: 128},
		{name: "overflow", value: "2147483648", want: 32},
		{name: "invalid", value: "many", want: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(name, tt.value)
			if got := readPositiveEnvInt32(name, 32); got != tt.want {
				t.Fatalf("readPositiveEnvInt32() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRuntimeLimitsInitialized(t *testing.T) {
	if agentOperationLimit <= 0 || cap(agentOperationSlots) != agentOperationLimit {
		t.Fatalf("agent operation limit = %d, channel capacity = %d", agentOperationLimit, cap(agentOperationSlots))
	}
	if maxCDPClientsPerProfile <= 0 {
		t.Fatalf("max CDP clients per profile = %d", maxCDPClientsPerProfile)
	}
}
