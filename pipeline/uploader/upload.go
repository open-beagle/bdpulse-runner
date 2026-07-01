package uploader

import (
	"context"
	"encoding/json"

	"github.com/open-beagle/bdpulse-go/drone"
	"github.com/open-beagle/bdpulse-runner/client"
	"github.com/open-beagle/bdpulse-runner/internal"
	"github.com/open-beagle/bdpulse-runner/pipeline"
)

var _ pipeline.Uploader = (*Upload)(nil)

type Upload struct {
	client client.Client
}

func New(client client.Client) *Upload {
	return &Upload{
		client: client,
	}
}

func (s *Upload) UploadCard(ctx context.Context, bytes []byte, state *pipeline.State, stepName string) error {
	src := state.Find(stepName)
	card := drone.CardInput{}
	err := json.Unmarshal(bytes, &card)
	if err != nil {
		return err
	}
	err = s.client.UploadCard(ctx, src.ID, &card)
	if err != nil {
		return err
	}
	// update step schema
	state.Lock()
	src.Schema = card.Schema
	cpy := internal.CloneStep(src)
	state.Unlock()
	err = s.client.UpdateStep(ctx, cpy)
	if err == nil {
		state.Lock()
		internal.MergeStep(cpy, src)
		state.Unlock()
	}
	return nil
}
