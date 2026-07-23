// Copyright 2019 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"testing"

	"github.com/open-beagle/bdpulse-runner/client"
	"github.com/open-beagle/bdpulse-runner/manifest"
)

type serverTaskResource struct{ raw *manifest.RawResource }

func (r *serverTaskResource) GetVersion() string { return r.raw.Version }
func (r *serverTaskResource) GetKind() string    { return r.raw.Kind }
func (r *serverTaskResource) GetType() string    { return r.raw.Type }
func (r *serverTaskResource) GetName() string    { return r.raw.Name }

func TestServerTask(t *testing.T) {
	manifest.Register(func(raw *manifest.RawResource) (manifest.Resource, bool, error) {
		if raw.Kind != manifest.KindPipeline {
			return nil, false, nil
		}
		return &serverTaskResource{raw: raw}, true, nil
	})
	tests := []struct {
		name     string
		file     *client.File
		selected bool
		resource string
		wantErr  bool
	}{
		{name: "legacy", selected: false},
		{
			name:     "selected",
			file:     &client.File{Data: []byte("kind: pipeline\nname: build\nsteps: []\n")},
			selected: true,
			resource: "build",
		},
		{
			name:     "multiple",
			file:     &client.File{Data: []byte("kind: pipeline\nname: first\n---\nkind: pipeline\nname: second\n")},
			selected: true,
			wantErr:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource, selected, err := serverTask(test.file)
			if (err != nil) != test.wantErr {
				t.Fatalf("serverTask() error = %v, wantErr %v", err, test.wantErr)
			}
			if selected != test.selected {
				t.Fatalf("serverTask() selected = %v, want %v", selected, test.selected)
			}
			if resource != nil && resource.GetName() != test.resource {
				t.Fatalf("serverTask() resource = %q, want %q", resource.GetName(), test.resource)
			}
		})
	}
}
