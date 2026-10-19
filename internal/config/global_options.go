package config

import "context"

// GlobalOptions reads the options of the user's global config and global
// data config, merged as a load merges them, without loading a project.
// It is for commands that run outside one, such as `crush remote
// launcher`, which reads options.remote_launch_target at each launch
// (desktop design 5-6, G-17).
func GlobalOptions(ctx context.Context) (*Options, error) {
	cfg, _, err := loadFromConfigPaths(ctx, []string{GlobalConfig(), GlobalConfigData()})
	if err != nil {
		return nil, err
	}
	if cfg.Options == nil {
		return &Options{}, nil
	}
	return cfg.Options, nil
}
