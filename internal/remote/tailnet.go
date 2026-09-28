package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// The remote server is reachable over Tailscale only: it listens on this
// machine's tailnet address, and every request must come from a device
// signed in to the same Tailscale account. Both facts come from the local
// tailscaled over its LocalAPI socket.

const localAPISocket = "/var/run/tailscale/tailscaled.sock"

var (
	tailnetV4 = netip.MustParsePrefix("100.64.0.0/10")
	tailnetV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// isTailnetAddr reports whether a is a Tailscale address.
func isTailnetAddr(a netip.Addr) bool {
	a = a.Unmap()
	return tailnetV4.Contains(a) || tailnetV6.Contains(a)
}

// localAPI talks to the local tailscaled.
type localAPI struct {
	client *http.Client
}

func newLocalAPI(socket string) *localAPI {
	return &localAPI{client: &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}}
}

func (l *localAPI) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://local-tailscaled.sock"+path, nil)
	if err != nil {
		return err
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tailscaled: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// self describes this machine on the tailnet.
type self struct {
	Host   string
	User   int64
	IPv4   netip.Addr
	Online bool
}

func (l *localAPI) self(ctx context.Context) (self, error) {
	var st struct {
		BackendState string
		Self         struct {
			HostName     string
			TailscaleIPs []netip.Addr
			UserID       int64
		}
	}
	if err := l.get(ctx, "/localapi/v0/status", &st); err != nil {
		return self{}, err
	}
	out := self{Host: st.Self.HostName, User: st.Self.UserID, Online: st.BackendState == "Running"}
	for _, ip := range st.Self.TailscaleIPs {
		if ip.Is4() {
			out.IPv4 = ip
			break
		}
	}
	return out, nil
}

// peer is who is on the other end of a tailnet connection.
type peer struct {
	User int64
	Name string
}

func (l *localAPI) whois(ctx context.Context, addr string) (peer, error) {
	var w struct {
		Node struct {
			ComputedName string
			Name         string
		}
		UserProfile struct {
			ID int64
		}
	}
	if err := l.get(ctx, "/localapi/v0/whois?addr="+url.QueryEscape(addr), &w); err != nil {
		return peer{}, err
	}
	name := w.Node.ComputedName
	if name == "" {
		name = w.Node.Name
	}
	return peer{User: w.UserProfile.ID, Name: name}, nil
}

// gate admits only devices of this machine's own Tailscale user. Answers
// are cached briefly per address, since the phone makes many requests.
type gate struct {
	whois func(ctx context.Context, addr string) (peer, error)
	user  int64

	mu    sync.Mutex
	cache map[netip.Addr]gateEntry
}

type gateEntry struct {
	peer peer
	ok   bool
	at   time.Time
}

const gateTTL = time.Minute

var errNotTailnet = errors.New("not a tailnet address")

func (g *gate) check(ctx context.Context, remoteAddr string) (peer, error) {
	ap, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return peer{}, err
	}
	ip := ap.Addr().Unmap()
	if !isTailnetAddr(ip) {
		return peer{}, errNotTailnet
	}
	g.mu.Lock()
	e, hit := g.cache[ip]
	g.mu.Unlock()
	if hit && time.Since(e.at) < gateTTL {
		if !e.ok {
			return e.peer, errors.New("device belongs to another Tailscale user")
		}
		return e.peer, nil
	}
	p, err := g.whois(ctx, net.JoinHostPort(ip.String(), strconv.Itoa(int(ap.Port()))))
	if err != nil {
		return peer{}, err
	}
	ok := p.User == g.user
	g.mu.Lock()
	if g.cache == nil {
		g.cache = map[netip.Addr]gateEntry{}
	}
	g.cache[ip] = gateEntry{peer: p, ok: ok, at: time.Now()}
	g.mu.Unlock()
	if !ok {
		return p, errors.New("device belongs to another Tailscale user")
	}
	return p, nil
}

// Ports are tried in order, so every Crush window gets its own and the
// phone only has to look at this small range.
const (
	firstPort = 7373
	lastPort  = 7392
)

// listenTailnet listens on the first free port of the range on ip.
func listenTailnet(ip netip.Addr) (net.Listener, error) {
	var lastErr error
	for port := firstPort; port <= lastPort; port++ {
		ln, err := net.Listen("tcp", netip.AddrPortFrom(ip, uint16(port)).String())
		if err == nil {
			return ln, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no free port in %d-%d: %w", firstPort, lastPort, lastErr)
}
