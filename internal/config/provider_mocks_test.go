package config

import (
	"context"

	"charm.land/catwalk/pkg/catwalk"
)

// mockCatwalkClient is a providers.CatwalkClient returning canned data.
type mockCatwalkClient struct {
	providers []catwalk.Provider
	err       error
	callCount int
}

func (m *mockCatwalkClient) GetProviders(ctx context.Context, etag string) ([]catwalk.Provider, error) {
	m.callCount++
	return m.providers, m.err
}

// mockHyperClient is a providers.HyperClient returning canned data.
type mockHyperClient struct {
	provider  catwalk.Provider
	err       error
	callCount int
}

func (m *mockHyperClient) Get(ctx context.Context, etag string) (catwalk.Provider, error) {
	m.callCount++
	return m.provider, m.err
}
