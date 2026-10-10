package agent

import (
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLinuxListeningPortsFindsOwnListener(t *testing.T) {
	t.Parallel()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	require.Contains(t, linuxListeningPorts([]int{os.Getpid()}), port)
}

func TestListeningInTableKeepsLocalListeners(t *testing.T) {
	t.Parallel()
	table := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:1435 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 11 1\n" +
		"   1: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12 1\n" +
		"   2: 0101A8C0:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 13 1\n" +
		"   3: 0100007F:1436 0100007F:9999 01 00000000:00000000 00:00000000 00000000  1000        0 14 1\n" +
		"   4: 0100007F:1437 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 99 1\n"
	inodes := map[string]bool{"11": true, "12": true, "13": true, "14": true}
	require.Equal(t, []int{0x1435, 0x1F90}, listeningInTable(table, inodes),
		"loopback and any-address listeners count; a LAN-only one, a connection and another program's socket don't")
	require.True(t, localAddress("00000000000000000000000001000000"))
	require.True(t, localAddress("0000000000000000FFFF00000100007F"))
	require.False(t, localAddress("0000000000000000FFFF00000101A8C0"))
}
