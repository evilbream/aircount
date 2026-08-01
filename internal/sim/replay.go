package sim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"poltergeist/internal/sensorwire"

	"github.com/rs/zerolog/log"
)

type Publisher interface {
	PublishBatch(batch sensorwire.Batch) error
}

type Replay struct {
	batches []sensorwire.Batch
}

func (r *Replay) loadBatches(dirname string) error {
	files, err := os.ReadDir(dirname)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		log.Info().Msgf("No replay files found in %s", dirname)
		return nil
	}

	if len(r.batches) == 0 {
		r.batches = make([]sensorwire.Batch, 0, len(files))
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dirname, file.Name()))
		if err != nil {
			return err
		}

		var batch sensorwire.Batch
		if err := json.Unmarshal(data, &batch); err != nil {
			return err
		}
		r.batches = append(r.batches, batch)
	}

	return nil
}

func (r *Replay) SendBatches(publisher Publisher) error {
	for _, batch := range r.batches {
		if err := publisher.PublishBatch(batch); err != nil {
			return err
		}
	}
	return nil
}
