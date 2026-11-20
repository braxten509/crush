package backend

import "github.com/charmbracelet/crush/internal/localmodel"

func (b *Backend) LocalModels() localmodel.Status { return localmodel.Default.Status() }

func (b *Backend) LoadLocalModel(id string) (localmodel.Status, error) {
	if err := localmodel.Default.Load(id); err != nil {
		return localmodel.Status{}, err
	}
	return localmodel.Default.Status(), nil
}

func (b *Backend) UnloadLocalModel() (localmodel.Status, error) {
	if err := localmodel.Default.Unload(); err != nil {
		return localmodel.Status{}, err
	}
	return localmodel.Default.Status(), nil
}

func (b *Backend) SizeLocalModel(id string, tokens int64) (localmodel.Status, error) {
	if err := localmodel.Default.SetSize(id, tokens); err != nil {
		return localmodel.Status{}, err
	}
	return localmodel.Default.Status(), nil
}
