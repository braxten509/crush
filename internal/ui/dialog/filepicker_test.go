package dialog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFilePickerPreviewReadyMsg checks that the preview flag is set on
// the update loop from a message, not from inside the transmit command.
func TestFilePickerPreviewReadyMsg(t *testing.T) {
	t.Parallel()

	f := &FilePicker{}
	require.Nil(t, f.HandleMsg(filePickerPreviewReadyMsg{}))
	require.True(t, f.previewingImage)
}
