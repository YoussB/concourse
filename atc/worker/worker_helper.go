package worker

import (
	"fmt"
	"path/filepath"

	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	"github.com/concourse/concourse/atc/db"
)

type workerHelper struct {
	gardenClient  garden.Client
	volumeClient  VolumeClient
	volumeRepo    db.VolumeRepository
	dbTeamFactory db.TeamFactory
	dbWorker      db.Worker
}

func (w workerHelper) findOrInitializeContainer(
	logger lager.Logger,
	owner db.ContainerOwner,
	metadata db.ContainerMetadata,
) (db.CreatingContainer, db.CreatedContainer, string, error) {

	creatingContainer, createdContainer, err := w.dbWorker.FindContainerOnWorker(owner)
	if err != nil {
		return nil, nil, "", err
	}

	var foundHandle string
	switch {
	case creatingContainer != nil:
		foundHandle = creatingContainer.Handle()
	case createdContainer != nil:
		foundHandle = createdContainer.Handle()
	}

	if foundHandle != "" {
		logger = logger.WithData(lager.Data{"container": foundHandle})
		logger.Debug("found-container-in-db")
		return creatingContainer, createdContainer, foundHandle, nil
	}

	// No foundHandle means no container in db
	logger.Debug("creating-container-in-db")
	creatingContainer, err = w.dbWorker.CreateContainer(
		owner,
		metadata,
	)
	if err != nil {
		logger.Error("failed-to-create-container-in-db", err)
		return nil, nil, "", err
	}

	foundHandle = creatingContainer.Handle()
	logger = logger.WithData(lager.Data{"container": foundHandle})
	logger.Debug("created-creating-container-in-db")

	return creatingContainer, nil, foundHandle, nil
}

func (w workerHelper) createGardenContainer(
	containerSpec ContainerSpec,
	fetchedImage FetchedImage,
	handleToCreate string,
	bindMounts []garden.BindMount,
) (garden.Container, error) {

	gardenProperties := garden.Properties{}

	if containerSpec.User != "" {
		gardenProperties[userPropertyName] = containerSpec.User
	} else {
		gardenProperties[userPropertyName] = fetchedImage.Metadata.User
	}

	env := append(fetchedImage.Metadata.Env, containerSpec.Env...)

	if w.dbWorker.HTTPProxyURL() != "" {
		env = append(env, fmt.Sprintf("http_proxy=%s", w.dbWorker.HTTPProxyURL()))
	}

	if w.dbWorker.HTTPSProxyURL() != "" {
		env = append(env, fmt.Sprintf("https_proxy=%s", w.dbWorker.HTTPSProxyURL()))
	}

	if w.dbWorker.NoProxy() != "" {
		env = append(env, fmt.Sprintf("no_proxy=%s", w.dbWorker.NoProxy()))
	}

	return w.gardenClient.Create(garden.ContainerSpec{
		Handle:     handleToCreate,
		RootFSPath: fetchedImage.URL,
		Privileged: fetchedImage.Privileged,
		BindMounts: bindMounts,
		Limits:     containerSpec.Limits.ToGardenLimits(),
		Env:        env,
		Properties: gardenProperties,
	})
}


func (w workerHelper) constructGardenWorkerContainer(
	logger lager.Logger,
	createdContainer db.CreatedContainer,
	gardenContainer garden.Container,
) (Container, error) {
	createdVolumes, err := w.volumeRepo.FindVolumesForContainer(createdContainer)
	if err != nil {
		logger.Error("failed-to-find-container-volumes", err)
		return nil, err
	}
	return newGardenWorkerContainer(
		logger,
		gardenContainer,
		createdContainer,
		createdVolumes,
		w.gardenClient,
		w.volumeClient,
		w.dbWorker.Name(),
	)
}

func anyMountTo(path string, destinationPaths []string) bool {
	for _, destinationPath := range destinationPaths {
		if filepath.Clean(destinationPath) == filepath.Clean(path) {
			return true
		}
	}

	return false
}

func getDestinationPathsFromInputs(inputs []InputSource) []string {
	destinationPaths := make([]string, len(inputs))

	for idx, input := range inputs {
		destinationPaths[idx] = input.DestinationPath()
	}

	return destinationPaths
}

func getDestinationPathsFromOutputs(outputs OutputPaths) []string {
	idx              := 0
	destinationPaths := make([]string, len(outputs))

	for _, destinationPath := range outputs {
		destinationPaths[idx] = destinationPath
		idx++
	}

	return destinationPaths
}
