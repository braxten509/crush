package skills

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInstallDirectoryFollowsEffectiveDiscoveryPolicy(t *testing.T) {
	shared := "/home/test/.agents/skills"
	override := "/tmp/excluded-skills"
	root, err := installationRoot(DiscoveryConfig{SkillsPaths: []string{shared}}, shared, override)
	require.NoError(t, err)
	require.Equal(t, shared, root)
	root, err = installationRoot(DiscoveryConfig{SkillsPaths: []string{override}}, shared, override)
	require.NoError(t, err)
	require.Equal(t, override, root)
	_, err = installationRoot(DiscoveryConfig{}, shared, override)
	require.Error(t, err)
}
