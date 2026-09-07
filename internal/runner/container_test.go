package runner

import (
	"reflect"
	"testing"
)

func baseSpec() ContainerSpec {
	return ContainerSpec{
		ContainerName: "run-1-reasoner-1-attempt-1",
		Image:         "engine-runner/adapter-claude:v1",
		Command:       []string{"claude", "run", "--prompt", "/workspace/prompt.md"},
		WorkingDir:    "/workspace",
		NetworkPolicy: NetworkDisabled,
		Mounts: []Mount{
			{HostPath: "/build/cache/run-1/reasoner-1-attempt-1/input", ContainerPath: "/workspace/input", ReadOnly: true},
			{HostPath: "/build/cache/run-1/reasoner-1-attempt-1/output", ContainerPath: "/workspace/output", ReadOnly: false},
		},
		Limits: ResourceLimits{CPUs: "1.0", MemoryBytes: 512 * 1024 * 1024, PIDsLimit: 64},
	}
}

func TestBuildDockerArgsExactCommandLine(t *testing.T) {
	spec := baseSpec()
	got, err := BuildDockerArgs(spec)
	if err != nil {
		t.Fatalf("BuildDockerArgs: %v", err)
	}
	want := []string{
		"run", "--rm", "--name", "run-1-reasoner-1-attempt-1",
		"--network", "none",
		"--cpus", "1.0",
		"--memory", "536870912",
		"--pids-limit", "64",
		"-w", "/workspace",
		"-v", "/build/cache/run-1/reasoner-1-attempt-1/input:/workspace/input:ro",
		"-v", "/build/cache/run-1/reasoner-1-attempt-1/output:/workspace/output",
		"engine-runner/adapter-claude:v1",
		"claude", "run", "--prompt", "/workspace/prompt.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildDockerArgs() =\n%v\nwant\n%v", got, want)
	}
}

func TestBuildDockerArgsReadOnlyRootFS(t *testing.T) {
	spec := baseSpec()
	spec.ReadOnlyRootFS = true
	got, err := BuildDockerArgs(spec)
	if err != nil {
		t.Fatalf("BuildDockerArgs: %v", err)
	}
	if !containsAdjacent(got, "--read-only") {
		t.Errorf("expected --read-only in args, got %v", got)
	}
}

func TestBuildDockerArgsEnvIsSortedDeterministically(t *testing.T) {
	spec := baseSpec()
	spec.Env = map[string]string{"ZEBRA": "1", "ALPHA": "2", "MIKE": "3"}

	var prev []string
	for i := 0; i < 5; i++ {
		got, err := BuildDockerArgs(spec)
		if err != nil {
			t.Fatalf("BuildDockerArgs: %v", err)
		}
		if prev != nil && !reflect.DeepEqual(got, prev) {
			t.Fatalf("BuildDockerArgs produced different output across calls with the same spec:\n%v\nvs\n%v", prev, got)
		}
		prev = got
	}
	want := []string{"-e", "ALPHA=2", "-e", "MIKE=3", "-e", "ZEBRA=1"}
	if !containsSubsequence(prev, want) {
		t.Errorf("expected env args in sorted order %v, got %v", want, prev)
	}
}

func TestNetworkPolicyMustBeExplicit(t *testing.T) {
	spec := baseSpec()
	spec.NetworkPolicy = ""
	if _, err := BuildDockerArgs(spec); err == nil {
		t.Fatalf("expected an error for an unset NetworkPolicy")
	}
}

func TestNetworkEnabledOmitsNetworkNoneFlag(t *testing.T) {
	spec := baseSpec()
	spec.NetworkPolicy = NetworkEnabled
	got, err := BuildDockerArgs(spec)
	if err != nil {
		t.Fatalf("BuildDockerArgs: %v", err)
	}
	if containsAdjacent(got, "none") {
		t.Errorf("did not expect \"--network none\" when NetworkPolicy is enabled, got %v", got)
	}
}

func TestValidateRejectsMissingRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*ContainerSpec)
	}{
		{"missing name", func(s *ContainerSpec) { s.ContainerName = "" }},
		{"missing image", func(s *ContainerSpec) { s.Image = "" }},
		{"missing command", func(s *ContainerSpec) { s.Command = nil }},
		{"missing mounts", func(s *ContainerSpec) { s.Mounts = nil }},
		{"empty mount host path", func(s *ContainerSpec) { s.Mounts[0].HostPath = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := baseSpec()
			tc.mod(&spec)
			if err := spec.Validate(); err == nil {
				t.Fatalf("expected an error for %s", tc.name)
			}
		})
	}
}

func TestValidateRejectsForbiddenMountPaths(t *testing.T) {
	forbidden := []string{
		"/home/alex/data/cases/happy-path",
		"/home/alex/agents/coder-engine-runner",
		"/home/alex/repos/engine-runner",
		"/var/run/docker.sock",
	}
	for _, path := range forbidden {
		t.Run(path, func(t *testing.T) {
			spec := baseSpec()
			spec.Mounts = append(spec.Mounts, Mount{HostPath: path, ContainerPath: "/forbidden", ReadOnly: true})
			if err := spec.Validate(); err == nil {
				t.Fatalf("expected Validate to reject mounting %s", path)
			}
		})
	}
}

func containsAdjacent(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

// containsSubsequence reports whether want appears as a contiguous
// subsequence of got.
func containsSubsequence(got, want []string) bool {
	if len(want) > len(got) {
		return false
	}
	for i := 0; i+len(want) <= len(got); i++ {
		match := true
		for j := range want {
			if got[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
