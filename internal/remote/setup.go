package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SetupState describes the next user-visible step, without containing login
// tokens, authentication URLs, or raw tailscaled error output.
type SetupState string

const (
	SetupReady       SetupState = "ready"
	SetupMissing     SetupState = "missing"
	SetupUnavailable SetupState = "unavailable"
	SetupLogin       SetupState = "login"
	SetupStopped     SetupState = "stopped"
	SetupApproval    SetupState = "approval"
	SetupWaiting     SetupState = "waiting"
	SetupUnsupported SetupState = "unsupported"
)

type SetupInfo struct {
	State  SetupState
	Title  string
	Detail string
	Action string
	Button string
}

type SetupError struct{ Info SetupInfo }

func (e *SetupError) Error() string { return e.Info.Title + ". " + e.Info.Detail }

// tailscaleExecutable also finds GUI installations whose CLI is not on PATH.
// Prefer the official app on macOS to avoid mixing a GUI and Homebrew daemon.
func tailscaleExecutable(platform string, lookPath func(string) (string, error)) (string, error) {
	if platform == "darwin" {
		for _, path := range []string{
			"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
			"/Applications/Tailscale.app/Contents/MacOS/tailscale",
		} {
			if found, err := lookPath(path); err == nil {
				return found, nil
			}
		}
	}
	return lookPath("tailscale")
}

func inspectSetup(ctx context.Context, platform string, api tailnetAPI, installed bool) SetupInfo {
	if platform != "linux" && platform != "darwin" {
		return SetupInfo{State: SetupUnsupported, Title: "Setup is available on macOS and Linux", Detail: "Set up Tailscale yourself on this computer, then try /remote again."}
	}
	st, err := api.status(ctx)
	if err != nil {
		if !installed {
			return SetupInfo{State: SetupMissing, Title: "Install Tailscale", Detail: "Tailscale connects this computer privately to Pocket Agents. The official installer may ask for your computer password.", Action: "install", Button: "Install Tailscale"}
		}
		detail := "Start the Tailscale service. Your computer may ask for your administrator password."
		if platform == "darwin" {
			detail = "Open Tailscale and allow its connection in macOS System Settings if asked. Then return here."
		}
		return SetupInfo{State: SetupUnavailable, Title: "Tailscale is not answering", Detail: detail, Action: "start", Button: "Start Tailscale"}
	}
	return setupFromStatus(st)
}

func setupFromStatus(st tailnetStatus) SetupInfo {
	switch st.BackendState {
	case "NeedsLogin":
		return SetupInfo{State: SetupLogin, Title: "Sign in to Tailscale", Detail: "Use the same Tailscale account as Pocket Agents. The next step shows a sign-in link and QR code in this terminal.", Action: "connect", Button: "Sign in"}
	case "Stopped":
		return SetupInfo{State: SetupStopped, Title: "Tailscale is turned off", Detail: "Connect with your saved Tailscale settings. Your computer may ask for your administrator password.", Action: "connect", Button: "Connect"}
	case "NeedsMachineAuth":
		return SetupInfo{State: SetupApproval, Title: "Approve this computer", Detail: "Approve this computer in your Tailscale account. If device signing is enabled, approve it from a trusted device too."}
	case "Running":
		me := st.self()
		if me.User <= 0 {
			return SetupInfo{State: SetupWaiting, Title: "A personal Tailscale account is needed", Detail: "This computer has no owning account. Sign in to Tailscale with the same personal account as Pocket Agents."}
		}
		if me.IPv4.IsValid() {
			return SetupInfo{State: SetupReady}
		}
		return SetupInfo{State: SetupWaiting, Title: "Waiting for a Tailscale address", Detail: "Check the connection in Tailscale. Sharing needs a Tailscale IPv4 address."}
	default:
		return SetupInfo{State: SetupWaiting, Title: "Tailscale is getting ready", Detail: "Finish any Tailscale permission prompts. Crush will check again automatically."}
	}
}

// CheckSetup never installs, opens an app, or changes network settings.
func CheckSetup(ctx context.Context) SetupInfo {
	_, err := tailscaleExecutable(runtime.GOOS, exec.LookPath)
	return inspectSetup(ctx, runtime.GOOS, newSystemAPI(), err == nil)
}

// RunSetupAction runs only after the owner selects the named action in the
// setup dialog. All password entry belongs to sudo or the system installer;
// credentials never pass through the agent, a command argument, or a log.
func RunSetupAction(ctx context.Context, action string, in io.Reader, out, stderr io.Writer) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return errors.New("guided setup supports macOS and Linux")
	}
	run := func(name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, stderr
		return cmd.Run()
	}
	switch action {
	case "install":
		if _, err := tailscaleExecutable(runtime.GOOS, exec.LookPath); err == nil {
			return nil // Never overwrite an existing installation.
		}
		return installTailscale(ctx, runtime.GOOS, out, run)
	case "start":
		if runtime.GOOS == "darwin" {
			fmt.Fprintln(out, "Allow Tailscale in System Settings if asked, then return to Crush.")
			return run("/usr/bin/open", "-a", "Tailscale")
		}
		name, args, err := serviceStartCommand(exec.LookPath)
		if err != nil {
			return err
		}
		return runElevated(run, name, args...)
	case "connect":
		path, err := tailscaleExecutable(runtime.GOOS, exec.LookPath)
		if err != nil {
			return errors.New("install Tailscale first, then try again")
		}
		fmt.Fprintln(out, "Sign in with the same Tailscale account as Pocket Agents.\nOpen the link below, or scan the QR code with your phone.\nPress Ctrl+C to cancel and return to Crush.")
		// No reset, forced login, DNS, route, SSH, operator or firewall flags.
		args := []string{"up", "--qr", "--timeout=5m"}
		if runtime.GOOS == "linux" {
			return runElevated(run, path, args...)
		}
		return run(path, args...)
	default:
		return errors.New("choose install, start, or connect")
	}
}

type setupRunner func(string, ...string) error

func runElevated(run setupRunner, name string, args ...string) error {
	if os.Geteuid() == 0 {
		return run(name, args...)
	}
	return run("sudo", append([]string{"--", name}, args...)...)
}

func serviceStartCommand(lookPath func(string) (string, error)) (string, []string, error) {
	if path, err := lookPath("systemctl"); err == nil {
		return path, []string{"start", "tailscaled"}, nil
	}
	if path, err := lookPath("rc-service"); err == nil {
		return path, []string{"tailscaled", "start"}, nil
	}
	return "", nil, errors.New("start the tailscaled service using this computer's service manager, then choose Check again")
}

func installTailscale(ctx context.Context, platform string, out io.Writer, run setupRunner) error {
	return installTailscaleWithDownload(ctx, platform, out, run, downloadSetupFile)
}

func installTailscaleWithDownload(ctx context.Context, platform string, out io.Writer, run setupRunner, download func(context.Context, string, string, int64) error) error {
	dir, err := os.MkdirTemp("", "crush-tailscale-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if platform == "darwin" {
		pkg := filepath.Join(dir, "Tailscale.pkg")
		fmt.Fprintln(out, "Downloading the official Tailscale installer…")
		if err := download(ctx, "https://pkgs.tailscale.com/stable/Tailscale-latest-macos.pkg", pkg, 150<<20); err != nil {
			return err
		}
		// Gatekeeper validates the installer before it can ask for privileges.
		if err := run("/usr/sbin/spctl", "--assess", "--type", "install", pkg); err != nil {
			return fmt.Errorf("macOS could not verify the installer; nothing installed: %w", err)
		}
		if err := runElevated(run, "/usr/sbin/installer", "-pkg", pkg, "-target", "/"); err != nil {
			return err
		}
		fmt.Fprintln(out, "Allow Tailscale in System Settings if asked, then return to Crush.")
		return run("/usr/bin/open", "-a", "Tailscale")
	}
	script := filepath.Join(dir, "install.sh")
	fmt.Fprintln(out, "Downloading Tailscale's official Linux installer.\nIt selects the package for this Linux distribution and may ask for your administrator password.")
	if err := download(ctx, "https://tailscale.com/install.sh", script, 1<<20); err != nil {
		return err
	}
	// Download completely before execution, rather than piping a partial
	// network response into a privileged shell. The official installer uses
	// each supported distribution's package verification and service manager.
	return run("sh", script)
}

func downloadSetupFile(ctx context.Context, address, destination string, limit int64) error {
	client := &http.Client{Timeout: 3 * time.Minute, CheckRedirect: installerRedirect}
	return downloadSetupFileWithClient(ctx, client, address, destination, limit)
}

func installerRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 || len(via) >= 5 || req.URL.Scheme != "https" || req.URL.Host != via[0].URL.Host {
		return errors.New("installer download redirected outside its official HTTPS host")
	}
	return nil
}

func downloadSetupFileWithClient(ctx context.Context, client *http.Client, address, destination string, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	if req.URL.Scheme != "https" {
		return errors.New("installer downloads require HTTPS")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("installer download failed: %s", resp.Status)
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(file, io.LimitReader(resp.Body, limit+1))
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if n == 0 || n > limit {
		return errors.New("installer download has an unexpected size")
	}
	if closeErr != nil {
		return closeErr
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return errors.New("installer download returned a web page")
	}
	return nil
}
