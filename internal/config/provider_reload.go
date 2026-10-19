package config

import (
	"fmt"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config/providers"
)

// ReloadModelCatalog makes the catalogs UpdateProviders and UpdateHyper
// cached the process's model catalog, which is otherwise read once per
// process, so the next load of a config sees them without a restart
// (desktop design 5-6, G-9). A catalog that was not cached is left as it
// is.
func ReloadModelCatalog() error {
	providerMu.Lock()
	defer providerMu.Unlock()
	isHyper := func(p catwalk.Provider) bool { return string(p.ID) == "hyper" }
	var list []catwalk.Provider
	if p, _, err := providers.NewCache[catwalk.Provider](cachePathFor("hyper")).Get(); err == nil && p.ID != "" {
		list = append(list, p)
	} else {
		for _, p := range providerList {
			if isHyper(p) {
				list = append(list, p)
			}
		}
	}
	if catalog, _, err := providers.NewCache[[]catwalk.Provider](cachePathFor("providers")).Get(); err == nil && len(catalog) > 0 {
		list = append(list, catalog...)
	} else {
		for _, p := range providerList {
			if !isHyper(p) {
				list = append(list, p)
			}
		}
	}
	if len(list) == 0 {
		return fmt.Errorf("no providers cached")
	}
	providerList = list
	return nil
}
