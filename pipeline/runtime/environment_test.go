package runtime

import (
	"strings"
	"testing"
)

func TestParseEnvironmentFile(t *testing.T) {
	values, err := parseEnvironmentFile([]byte("# comment\nBUILD_VERSION=v7.3.0\nBUILD_VERSION=v7.3.1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := values["BUILD_VERSION"], "v7.3.1"; got != want {
		t.Fatalf("BUILD_VERSION = %q, want %q", got, want)
	}
}

func TestInheritedEnvironmentRejectsConflict(t *testing.T) {
	left := &environmentStep{name: "left"}
	right := &environmentStep{name: "right"}
	consumer := &environmentStep{name: "publish", dependencies: []string{"left", "right"}}
	environment := &environmentState{outputs: map[string]map[string]string{
		"left":  {"BUILD_VERSION": "v7.3.0"},
		"right": {"BUILD_VERSION": "v7.3.1"},
	}}
	_, err := environment.inheritedEnvironment(environmentSpec{left, right, consumer}, consumer)
	if err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("error = %v, want conflict error", err)
	}
}

func TestApplyEnvironmentResolvesDynamicExpression(t *testing.T) {
	producer := &environmentStep{name: "version"}
	consumer := &environmentStep{
		name:         "publish",
		dependencies: []string{"version"},
		environment: map[string]string{
			"PLUGIN_VERSION":          "${{ env.BUILD_VERSION }}",
			ScriptEnvironmentVariable: "echo ${{ env.BUILD_VERSION }}",
		},
		image: "registry.example/build:${{ env.BUILD_VERSION }}",
	}
	environment := &environmentState{outputs: map[string]map[string]string{
		"version": {"BUILD_VERSION": "v7.3.0"},
	}}
	exec := &Execer{}
	if err := exec.applyEnvironment(environment, environmentSpec{producer, consumer}, consumer, consumer, consumer.environment); err != nil {
		t.Fatal(err)
	}
	if got, want := consumer.image, "registry.example/build:v7.3.0"; got != want {
		t.Fatalf("image = %q, want %q", got, want)
	}
	if got, want := consumer.environment["PLUGIN_VERSION"], "v7.3.0"; got != want {
		t.Fatalf("PLUGIN_VERSION = %q, want %q", got, want)
	}
	if got, want := consumer.environment[ScriptEnvironmentVariable], "echo v7.3.0"; got != want {
		t.Fatalf("DRONE_SCRIPT = %q, want %q", got, want)
	}
}

type environmentSpec []*environmentStep

func (s environmentSpec) StepAt(index int) Step { return s[index] }
func (s environmentSpec) StepLen() int          { return len(s) }

type environmentStep struct {
	name         string
	dependencies []string
	environment  map[string]string
	overrides    map[string]bool
	image        string
}

func (s *environmentStep) GetName() string                          { return s.name }
func (s *environmentStep) GetDependencies() []string                { return s.dependencies }
func (s *environmentStep) GetEnviron() map[string]string            { return s.environment }
func (s *environmentStep) SetEnviron(environment map[string]string) { s.environment = environment }
func (s *environmentStep) GetEnvironmentOverrides() map[string]bool { return s.overrides }
func (s *environmentStep) GetErrPolicy() ErrPolicy                  { return ErrIgnore }
func (s *environmentStep) GetRunPolicy() RunPolicy                  { return RunAlways }
func (s *environmentStep) GetSecretAt(int) Secret                   { return nil }
func (s *environmentStep) GetSecretLen() int                        { return 0 }
func (s *environmentStep) IsDetached() bool                         { return false }
func (s *environmentStep) GetImage() string                         { return s.image }
func (s *environmentStep) SetImage(image string)                    { s.image = image }
func (s *environmentStep) Clone() Step {
	copy := *s
	copy.environment = map[string]string{}
	for key, value := range s.environment {
		copy.environment[key] = value
	}
	return &copy
}
