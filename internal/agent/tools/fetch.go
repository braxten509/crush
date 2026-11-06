package tools

// The fetch tool is not in this package. It lives in package webtool
// (internal/agent/tools/webtool/fetch.go), which imports this one, so
// nothing here can refer back to it. Its shared types (FetchParams,
// FetchPermissionsParams, FetchToolName, MaxFetchSize) stay in this
// package, in web_types.go.
//
// Upstream Crush still keeps the tool in this file, so upstream changes to
// it arrive here when merging and need porting to webtool/fetch.go.
