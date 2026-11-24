package secureentry

// KDE Wallet entries (owner, 2026-11-24): a secure field can save into the
// user's wallet instead of a prepared file. The wallet keeps the value
// encrypted on disk and takes any text, line breaks included. Values go to
// kwallet-query on its standard input, never as an argument, so they never
// show in the process list.

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode"
)

// walletTool is the program that reads and writes the wallet; tests point
// it at a stand-in.
var walletTool = "kwallet-query"

// walletName is the user's wallet.
const walletName = "kdewallet"

// walletWait is the longest a wallet call may take: a closed wallet asks
// for its password first.
const walletWait = 2 * time.Minute

// normalizeWallet checks a wallet spec's metadata and fills in its defaults.
func (spec Spec) normalizeWallet() (Spec, error) {
	w := *spec.Wallet
	w.Folder = strings.TrimSpace(w.Folder)
	w.Entry = strings.TrimSpace(w.Entry)
	if spec.File != "" {
		return spec, errors.New("give a secure entry either a file or a wallet entry, not both")
	}
	if w.Folder == "" || w.Entry == "" {
		return spec, errors.New("a wallet entry needs a folder and an entry name")
	}
	if strings.IndexFunc(w.Folder+w.Entry+spec.Label, unicode.IsControl) >= 0 {
		return spec, errors.New("entry metadata cannot contain control characters")
	}
	if strings.HasPrefix(w.Folder, "-") || strings.HasPrefix(w.Entry, "-") {
		return spec, errors.New("a wallet folder or entry name cannot start with -")
	}
	if spec.Label == "" {
		spec.Label = "Secret"
	}
	spec.Wallet = &w
	return spec, nil
}

// key names a wallet entry, for telling two fields apart.
func (w WalletSpec) key() string { return w.Folder + "\x00" + w.Entry }

// walletHas reports whether the wallet already holds the entry.
func walletHas(w WalletSpec) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), walletWait)
	defer cancel()
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, walletTool, "-f", w.Folder, "-l", walletName)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		// A folder that does not exist yet holds no entries.
		if strings.Contains(errOut.String()+out.String(), "does not exist") {
			return false, nil
		}
		return false, errors.New("cannot open KDE Wallet")
	}
	return slices.Contains(strings.Split(out.String(), "\n"), w.Entry), nil
}

// checkWalletFree refuses an entry that already exists, unless the spec
// says it replaces it.
func checkWalletFree(w WalletSpec) error {
	if w.Replace {
		return nil
	}
	has, err := walletHas(w)
	if err != nil {
		return err
	}
	if has {
		return errors.New("KDE Wallet already has this entry; ask again with \"replace\": true to change it")
	}
	return nil
}

// walletWrite saves value into the entry, handing it over on standard input.
func walletWrite(w WalletSpec, value []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), walletWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, walletTool, "-f", w.Folder, "-w", w.Entry, walletName)
	cmd.Stdin = bytes.NewReader(value)
	if cmd.Run() != nil {
		return errors.New("cannot save into KDE Wallet")
	}
	return nil
}
