# proj

A throwaway project for the desktop app's window checks. `tests/run-in-sandbox.sh`
copies this folder to `/tmp/home/proj` inside the sandbox, runs `git init` and
writes `crush.json` from `crush.json.in` with the fake AI server's port.

The `auth` package keeps a sign-in session in a cookie.
