// Copyright 2019 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/open-beagle/bdpulse-go/drone"
	"github.com/open-beagle/bdpulse-runner/pipeline"
)

func TestExecIsolatesConcurrentPipelineEnvironment(t *testing.T) {
	reporter := &isolationReporter{
		exported: make(chan struct{}),
		release:  make(chan struct{}),
	}
	defer reporter.unblock()
	engine := &isolationEngine{
		pipelineBSetup: make(chan struct{}),
		consumerValue:  make(chan string, 1),
	}
	exec := NewExecer(
		reporter,
		pipeline.NopStreamer(),
		pipeline.NopUploader(),
		engine,
		0,
	)

	pipelineA := environmentSpec{
		&environmentStep{
			name: "version",
			environment: map[string]string{
				EnvironmentFileVariable: EnvironmentFilePath,
				"PIPELINE_ID":           "A",
			},
		},
		&environmentStep{
			name:         "publish",
			dependencies: []string{"version"},
			environment: map[string]string{
				"PIPELINE_ID":    "A",
				"PLUGIN_VERSION": "${{ env.BUILD_VERSION }}",
			},
		},
	}
	pipelineB := environmentSpec{
		&environmentStep{
			name:        "noop",
			environment: map[string]string{"PIPELINE_ID": "B"},
		},
	}

	doneA := make(chan error, 1)
	go func() {
		doneA <- exec.Exec(context.Background(), pipelineA, isolationPipelineState(1, pipelineA))
	}()
	waitForSignal(t, reporter.exported, "pipeline A environment export")

	doneB := make(chan error, 1)
	go func() {
		doneB <- exec.Exec(context.Background(), pipelineB, isolationPipelineState(2, pipelineB))
	}()
	waitForSignal(t, engine.pipelineBSetup, "pipeline B setup")
	if err := waitForResult(t, doneB, "pipeline B completion"); err != nil {
		t.Fatal(err)
	}

	reporter.unblock()
	if err := waitForResult(t, doneA, "pipeline A completion"); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-engine.consumerValue:
		if want := "v7.3.0"; got != want {
			t.Fatalf("pipeline A PLUGIN_VERSION = %q, want %q", got, want)
		}
	default:
		t.Fatal("pipeline A publish step did not run")
	}
}

func TestExec(t *testing.T) {
	t.Skip()
}

func TestExec_NonZeroExit(t *testing.T) {
	t.Skip()
}

func TestExec_Exit78(t *testing.T) {
	t.Skip()
}

func TestExec_Error(t *testing.T) {
	t.Skip()
}

func TestExec_CtxError(t *testing.T) {
	t.Skip()
}

func TestExec_ReportError(t *testing.T) {
	t.Skip()
}

func TestExec_SkipCtxDone(t *testing.T) {
	t.Skip()
}

type isolationReporter struct {
	exported    chan struct{}
	release     chan struct{}
	exportedOne sync.Once
	releaseOne  sync.Once
}

func (r *isolationReporter) ReportStage(context.Context, *pipeline.State) error {
	return nil
}

func (r *isolationReporter) ReportStep(_ context.Context, state *pipeline.State, name string) error {
	state.Lock()
	stageID := state.Stage.ID
	status := findStep(state, name).Status
	state.Unlock()
	if stageID == 1 && name == "version" && status == drone.StatusPassing {
		r.exportedOne.Do(func() { close(r.exported) })
		<-r.release
	}
	return nil
}

func (r *isolationReporter) unblock() {
	r.releaseOne.Do(func() { close(r.release) })
}

type isolationEngine struct {
	pipelineBSetup chan struct{}
	consumerValue  chan string
	setupOne       sync.Once
}

func (e *isolationEngine) Setup(_ context.Context, spec Spec) error {
	if pipelineID(spec.StepAt(0)) == "B" {
		e.setupOne.Do(func() { close(e.pipelineBSetup) })
	}
	return nil
}

func (*isolationEngine) Destroy(context.Context, Spec) error {
	return nil
}

func (e *isolationEngine) Run(_ context.Context, _ Spec, step Step, _ io.Writer) (*State, error) {
	if pipelineID(step) == "A" && step.GetName() == "publish" {
		e.consumerValue <- step.GetEnviron()["PLUGIN_VERSION"]
	}
	return &State{ExitCode: 0, Exited: true}, nil
}

func (*isolationEngine) ReadFile(_ context.Context, _ Spec, step Step, _ string) ([]byte, error) {
	if pipelineID(step) != "A" || step.GetName() != "version" {
		return nil, fmt.Errorf("unexpected environment export from pipeline %q step %q", pipelineID(step), step.GetName())
	}
	return []byte("BUILD_VERSION=v7.3.0\n"), nil
}

func pipelineID(step Step) string {
	return step.GetEnviron()["PIPELINE_ID"]
}

func isolationPipelineState(id int64, spec environmentSpec) *pipeline.State {
	steps := make([]*drone.Step, 0, len(spec))
	for _, step := range spec {
		steps = append(steps, &drone.Step{
			Name:   step.name,
			Status: drone.StatusPending,
		})
	}
	return &pipeline.State{
		Build: &drone.Build{},
		Stage: &drone.Stage{
			ID:     id,
			Status: drone.StatusRunning,
			Steps:  steps,
		},
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForResult(t *testing.T, result <-chan error, description string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}
