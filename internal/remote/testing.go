package remote

import (
	"context"
	"net"
)

// NewTestAction builds an Action with a reply channel, for tests in other
// packages. The channel receives what [Action.Done] reports.
func NewTestAction(a Action) (*Action, <-chan error) {
	reply := make(chan error, 1)
	a.reply = reply
	return &a, reply
}

// testPhoneAddr is where every request to a test share seems to come
// from: a tailnet address of the owner's own phone.
var testPhoneAddr = &net.TCPAddr{IP: net.IPv4(100, 64, 0, 7), Port: 40000}

// StartTestShare runs a share of src for tests in other packages, without
// Tailscale. It listens on a loopback address, and every request seems to
// come from the owner's own phone, so a test reads the share as the phone
// does: GET url+"/v1/events" streams what the phone sees. Stop the server
// when done; that closes the address too.
func StartTestShare(src Source, launchID string) (*Server, string, error) {
	const owner = 1
	whois := func(context.Context, string) (peer, error) {
		return peer{User: owner, Name: "test phone"}, nil
	}
	var url string
	srv, err := start(src, self{Host: "test", User: owner, Online: true}, whois, func() (net.Listener, error) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		url = "http://" + ln.Addr().String()
		return phoneListener{ln}, nil
	}, launchID)
	if err != nil {
		return nil, "", err
	}
	return srv, url, nil
}

// phoneListener makes every connection seem to come from testPhoneAddr.
type phoneListener struct{ net.Listener }

func (l phoneListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return phoneConn{c}, nil
}

type phoneConn struct{ net.Conn }

func (phoneConn) RemoteAddr() net.Addr { return testPhoneAddr }
