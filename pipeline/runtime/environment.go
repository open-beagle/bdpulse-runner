package runtime

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

const (
	EnvironmentFileVariable      = "CI_ENV"
	DroneEnvironmentFileVariable = "DRONE_ENV"
	ScriptEnvironmentVariable    = "DRONE_SCRIPT"
	EnvironmentFilePath          = "/tmp/awecloud-ci/env"
	EnvironmentFilePathWindows   = "C:\\awecloud-ci\\env"

	maxEnvironmentFileSize  = 64 * 1024
	maxEnvironmentVariables = 256
	maxEnvironmentKeySize   = 128
	maxEnvironmentValueSize = 8 * 1024
)

var (
	environmentKey        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	environmentExpression = regexp.MustCompile(`\$\{\{\s*env\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
)

type environmentState struct {
	mu      sync.Mutex
	outputs map[string]map[string]string
}

func newEnvironmentState() *environmentState {
	return &environmentState{outputs: map[string]map[string]string{}}
}

func (e *Execer) applyEnvironment(environment *environmentState, spec Spec, original, copy Step, envs map[string]string) error {
	dynamic, err := environment.inheritedEnvironment(spec, original)
	if err != nil {
		return err
	}
	overrides := environmentOverrides(copy)
	for key, value := range dynamic {
		if !overrides[key] {
			envs[key] = value
		}
	}
	image, err := resolveEnvironmentExpressions(copy.GetImage(), envs)
	if err != nil {
		return fmt.Errorf("step %q image: %w", copy.GetName(), err)
	}
	if image != copy.GetImage() {
		step, ok := copy.(interface{ SetImage(string) })
		if !ok {
			return fmt.Errorf("step %q does not support image expressions", copy.GetName())
		}
		step.SetImage(image)
	}
	if script, ok := envs[ScriptEnvironmentVariable]; ok {
		resolved, err := resolveEnvironmentExpressions(script, envs)
		if err != nil {
			return fmt.Errorf("step %q script: %w", copy.GetName(), err)
		}
		envs[ScriptEnvironmentVariable] = resolved
	}
	for key, value := range envs {
		if !strings.HasPrefix(key, "PLUGIN_") {
			continue
		}
		resolved, err := resolveEnvironmentExpressions(value, envs)
		if err != nil {
			return fmt.Errorf("step %q setting %q: %w", copy.GetName(), key, err)
		}
		envs[key] = resolved
	}
	copy.SetEnviron(envs)
	return nil
}

func (e *Execer) collectEnvironment(ctx context.Context, environment *environmentState, spec Spec, step Step) error {
	path, ok := step.GetEnviron()[EnvironmentFileVariable]
	if !ok {
		return nil
	}
	reader, ok := e.engine.(interface {
		ReadFile(context.Context, Spec, Step, string) ([]byte, error)
	})
	if !ok {
		return nil
	}
	data, err := reader.ReadFile(ctx, spec, step, path)
	if err != nil {
		return fmt.Errorf("step %q cannot export %s: %w", step.GetName(), EnvironmentFileVariable, err)
	}
	values, err := parseEnvironmentFile(data)
	if err != nil {
		return fmt.Errorf("step %q invalid %s: %w", step.GetName(), EnvironmentFileVariable, err)
	}
	environment.mu.Lock()
	environment.outputs[step.GetName()] = values
	environment.mu.Unlock()
	return nil
}

func environmentOverrides(step Step) map[string]bool {
	if step, ok := step.(interface{ GetEnvironmentOverrides() map[string]bool }); ok {
		return step.GetEnvironmentOverrides()
	}
	return nil
}

func (e *environmentState) inheritedEnvironment(spec Spec, step Step) (map[string]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	values := map[string]string{}
	merge := func(name string) error {
		for key, value := range e.outputs[name] {
			if existing, ok := values[key]; ok && existing != value {
				return fmt.Errorf("step %q receives conflicting environment variable %q", step.GetName(), key)
			}
			values[key] = value
		}
		return nil
	}
	steps := map[string]Step{}
	for i := 0; i < spec.StepLen(); i++ {
		candidate := spec.StepAt(i)
		steps[candidate.GetName()] = candidate
	}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		visited[name] = true
		candidate, ok := steps[name]
		if !ok {
			return nil
		}
		for _, dependency := range candidate.GetDependencies() {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		return merge(name)
	}
	for _, dependency := range step.GetDependencies() {
		if err := visit(dependency); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func parseEnvironmentFile(data []byte) (map[string]string, error) {
	if len(data) > maxEnvironmentFileSize {
		return nil, fmt.Errorf("file exceeds %d bytes", maxEnvironmentFileSize)
	}
	values := map[string]string{}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		parts := bytes.SplitN(line, []byte{'='}, 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("expected KEY=VALUE")
		}
		key, value := string(parts[0]), string(parts[1])
		if !environmentKey.MatchString(key) {
			return nil, fmt.Errorf("invalid variable name %q", key)
		}
		if isProtectedEnvironment(key) {
			return nil, fmt.Errorf("protected variable %q", key)
		}
		if len(key) > maxEnvironmentKeySize || len(value) > maxEnvironmentValueSize {
			return nil, fmt.Errorf("variable %q exceeds size limit", key)
		}
		if _, ok := values[key]; !ok && len(values) == maxEnvironmentVariables {
			return nil, fmt.Errorf("file exceeds %d variables", maxEnvironmentVariables)
		}
		values[key] = value
	}
	return values, nil
}

func resolveEnvironmentExpressions(value string, environment map[string]string) (string, error) {
	var unresolved string
	resolved := environmentExpression.ReplaceAllStringFunc(value, func(expression string) string {
		parts := environmentExpression.FindStringSubmatch(expression)
		result, ok := environment[parts[1]]
		if !ok {
			unresolved = parts[1]
			return expression
		}
		return result
	})
	if unresolved != "" {
		return "", fmt.Errorf("undefined environment variable %q", unresolved)
	}
	return resolved, nil
}

func isProtectedEnvironment(key string) bool {
	for _, prefix := range []string{"DRONE_", "CI_", "AWE_", "PLUGIN_"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
