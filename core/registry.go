package core

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// The vault registry is a machine-global list of the vaults hebb knows about,
// stored at $XDG_CONFIG_HOME/hebb/vaults.toml (else ~/.config/hebb/vaults.toml).
// It is what lets one process (the web server) enumerate and switch between
// vaults; install/new register a vault here, reset deregisters it. It is
// advisory: a missing or unreadable registry is treated as empty, never fatal.

// VaultRef is one registered vault: a display name and its canonical path.
type VaultRef struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

// Registry is the committed set of known vaults.
type Registry struct {
	Vaults []VaultRef `toml:"vault"`
}

// RegistryPath returns the registry file path for the given home directory,
// honouring $XDG_CONFIG_HOME.
func RegistryPath(home string) string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "hebb", "vaults.toml")
}

// LoadRegistry reads the registry at path. A missing file yields an empty
// registry (not an error); a malformed file is an error.
func LoadRegistry(path string) (Registry, error) {
	var r Registry
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err := toml.Unmarshal(data, &r); err != nil {
		return r, err
	}
	return r, nil
}

// Add registers a vault by canonical path, updating the name if the path is
// already present. Returns true if the registry changed. The path is made
// absolute and symlink-resolved so the same vault is never listed twice under
// different spellings.
func (r *Registry) Add(name, vaultPath string) bool {
	canon := canonicalVaultPath(vaultPath)
	for i := range r.Vaults {
		if r.Vaults[i].Path == canon {
			if r.Vaults[i].Name == name {
				return false
			}
			r.Vaults[i].Name = name
			return true
		}
	}
	r.Vaults = append(r.Vaults, VaultRef{Name: name, Path: canon})
	return true
}

// Remove deregisters a vault by path. Returns true if an entry was removed.
func (r *Registry) Remove(vaultPath string) bool {
	canon := canonicalVaultPath(vaultPath)
	out := r.Vaults[:0]
	removed := false
	for _, v := range r.Vaults {
		if v.Path == canon {
			removed = true
			continue
		}
		out = append(out, v)
	}
	r.Vaults = out
	return removed
}

// Save writes the registry to path, creating the parent dir. Idempotent.
func (r Registry) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# hebb vault registry - the vaults on this machine.\n")
	buf.WriteString("# Managed by 'hebb install'/'hebb reset'. Safe to edit.\n\n")
	if err := toml.NewEncoder(&buf).Encode(r); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// RegisterVault adds (or refreshes) a vault in the registry at path and saves it
// only when something changed. A convenience for install/new.
func RegisterVault(registryPath, name, vaultPath string) error {
	r, err := LoadRegistry(registryPath)
	if err != nil {
		return err
	}
	if r.Add(name, vaultPath) {
		return r.Save(registryPath)
	}
	return nil
}

// DeregisterVault removes a vault from the registry at path, saving only when it
// changed. A convenience for reset.
func DeregisterVault(registryPath, vaultPath string) error {
	r, err := LoadRegistry(registryPath)
	if err != nil {
		return err
	}
	if r.Remove(vaultPath) {
		return r.Save(registryPath)
	}
	return nil
}

// canonicalVaultPath returns an absolute, symlink-resolved path, falling back to
// the absolute path (then the input) when resolution fails, so an entry is
// always stored under one stable spelling.
func canonicalVaultPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}
