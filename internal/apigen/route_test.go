package apigen

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeRoute is a minimal [Route] for exercising [For].
type fakeRoute struct {
	method, path string
	status       int
	req, resp    any
}

func (r fakeRoute) RouteMethod() string  { return r.method }
func (r fakeRoute) RoutePath() string    { return r.path }
func (r fakeRoute) RouteStatus() int     { return r.status }
func (r fakeRoute) RequestExample() any  { return r.req }
func (r fakeRoute) ResponseExample() any { return r.resp }

func TestForTakesRoutingAndSchemasFromRoute(t *testing.T) {
	t.Parallel()
	called := false
	r := fakeRoute{method: http.MethodPost, path: "/v1/things", status: http.StatusAccepted, req: sample{}, resp: nested{}}

	e := For(r).
		Summary("Create thing").
		Bind(func(http.ResponseWriter, *http.Request) { called = true }).
		Tags("things").
		Build()

	require.Equal(t, http.MethodPost, e.Method())
	require.Equal(t, "/v1/things", e.Path())
	require.Equal(t, http.StatusAccepted, e.Status())
	require.Equal(t, sample{}, e.Request())
	require.Equal(t, nested{}, e.Response())
	require.Equal(t, "Create thing", e.Summary())
	require.Equal(t, []string{"things"}, e.Tags())
	e.Handler()(nil, nil)
	require.True(t, called)
}

func TestForWithoutBodies(t *testing.T) {
	t.Parallel()
	e := For(fakeRoute{method: http.MethodGet, path: "/v1/health", status: http.StatusOK}).
		Handle(func(http.ResponseWriter, *http.Request) {})
	require.Nil(t, e.Request())
	require.Nil(t, e.Response())
}

func TestBuildWithoutHandlerPanics(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() { For(fakeRoute{method: http.MethodGet, path: "/x"}).Build() })
}
