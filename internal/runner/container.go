package runner

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// NetworkPolicy makes "is this container's network disabled" an explicit,
// validated choice rather than a zero-value default. Section 9: "Network
// disabled unless a vendor invocation specifically requires controlled
// egress." A ContainerSpec with an unset NetworkPolicy fails validation
// instead of silently running with network access.
type NetworkPolicy string

const (
	NetworkDisabled NetworkPolicy = "disabled"
	NetworkEnabled  NetworkPolicy = "enabled"
)

// Mount is one bind mount into the container.
type Mount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// ResourceLimits are the CPU/memory/process bounds section 9 requires. A
// zero value in any field means "no limit set" - callers building a real
// stage container should always set all three; tests exercising the
// command-building logic in isolation are free to leave them unset.
type ResourceLimits struct {
	CPUs        string // docker --cpus value, e.g. "1.0"
	MemoryBytes int64  // docker --memory, in bytes
	PIDsLimit   int64  // docker --pids-limit
}

// ContainerSpec is section 9's isolation contract expressed as a value
// BuildDockerArgs can turn into an exact command line. This is the
// primitive Milestone 2's real vendor adapters use to execute their CLI
// invocations in isolation; FixtureAdapter does not use it; it runs safe,
// deterministic, in-process Go code with no untrusted execution or network
// access to isolate from, so wrapping it in a container would add
// complexity without a corresponding safety benefit.
type ContainerSpec struct {
	// ContainerName must be unique per attempt, never reused across
	// cases, stages, or attempts - the container-level analogue of
	// section 9's "no reuse of vendor conversation IDs."
	ContainerName string

	Image      string
	Command    []string
	WorkingDir string
	Mounts     []Mount

	// Env carries non-secret values only. Credentials are never modeled
	// here; section 9 requires them injected as scoped secrets, never
	// written into a spec that might be logged or replayed.
	Env map[string]string

	NetworkPolicy  NetworkPolicy
	ReadOnlyRootFS bool
	Limits         ResourceLimits

	// Stdin is piped to the container's standard input. It carries the
	// prompt: a prompt passed as an argv element is bounded by the
	// kernel's per-argument limit (MAX_ARG_STRLEN, 128 KiB on Linux), and
	// a real case diff can exceed it - the container then fails with
	// "argument list too long" before any model call, so the failure is
	// free but the case cannot run at all. Stdin has no such limit.
	//
	// When set, BuildDockerArgs adds -i so docker keeps stdin open.
	Stdin []byte
}

// forbiddenHostPathPrefixes are the exact paths section 9 says must never
// be mounted into a stage container. This is defense in depth, not the
// primary boundary - the primary boundary is that callers should only ever
// build Mounts from a fresh per-attempt workspace in the first place.
var forbiddenHostPathPrefixes = []string{
	"/home/alex/agents",
	"/home/alex/data",
	"/home/alex/repos",
	"/var/run/docker.sock",
}

func (s ContainerSpec) Validate() error {
	if s.ContainerName == "" {
		return fmt.Errorf("container: ContainerName is required")
	}
	if s.Image == "" {
		return fmt.Errorf("container: Image is required")
	}
	if len(s.Command) == 0 {
		return fmt.Errorf("container: Command must not be empty")
	}
	if s.NetworkPolicy != NetworkDisabled && s.NetworkPolicy != NetworkEnabled {
		return fmt.Errorf("container: NetworkPolicy must be explicitly %q or %q, got %q", NetworkDisabled, NetworkEnabled, s.NetworkPolicy)
	}
	if len(s.Mounts) == 0 {
		return fmt.Errorf("container: at least one mount is required")
	}
	for _, m := range s.Mounts {
		if m.HostPath == "" || m.ContainerPath == "" {
			return fmt.Errorf("container: mount has an empty path: %+v", m)
		}
		abs, err := filepath.Abs(m.HostPath)
		if err != nil {
			return fmt.Errorf("container: resolving mount %s: %w", m.HostPath, err)
		}
		for _, forbidden := range forbiddenHostPathPrefixes {
			if abs == forbidden || strings.HasPrefix(abs, forbidden+"/") {
				return fmt.Errorf("container: refusing to mount forbidden host path %s", abs)
			}
		}
	}
	return nil
}

// BuildDockerArgs turns a validated ContainerSpec into the exact argument
// list for `docker run`. It is a pure function: the same spec always
// produces the same arguments in the same order, so the resulting command
// line is itself a reproducible, loggable artifact - and testable without a
// running daemon.
func BuildDockerArgs(spec ContainerSpec) ([]string, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	args := []string{"run", "--rm", "--name", spec.ContainerName}

	// -i keeps stdin open so a piped prompt reaches the CLI.
	if len(spec.Stdin) > 0 {
		args = append(args, "-i")
	}

	if spec.NetworkPolicy == NetworkDisabled {
		args = append(args, "--network", "none")
	}
	if spec.ReadOnlyRootFS {
		args = append(args, "--read-only")
	}
	if spec.Limits.CPUs != "" {
		args = append(args, "--cpus", spec.Limits.CPUs)
	}
	if spec.Limits.MemoryBytes > 0 {
		args = append(args, "--memory", strconv.FormatInt(spec.Limits.MemoryBytes, 10))
	}
	if spec.Limits.PIDsLimit > 0 {
		args = append(args, "--pids-limit", strconv.FormatInt(spec.Limits.PIDsLimit, 10))
	}
	if spec.WorkingDir != "" {
		args = append(args, "-w", spec.WorkingDir)
	}

	for _, m := range spec.Mounts {
		mountArg := fmt.Sprintf("%s:%s", m.HostPath, m.ContainerPath)
		if m.ReadOnly {
			mountArg += ":ro"
		}
		args = append(args, "-v", mountArg)
	}

	envKeys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		args = append(args, "-e", k+"="+spec.Env[k])
	}

	args = append(args, spec.Image)
	args = append(args, spec.Command...)
	return args, nil
}
