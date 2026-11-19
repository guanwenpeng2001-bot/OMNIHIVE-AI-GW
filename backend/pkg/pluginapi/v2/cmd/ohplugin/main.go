// Command ohplugin signs native v2 packages and rebuilds verified v1 packages offline.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: ohplugin keygen|sign|migrate [flags]")
	}
	mode := os.Args[1]
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	src := flags.String("src", "", "package directory (sign) or original v1 archive (migrate)")
	out := flags.String("out", "", "new .ohplugin path")
	keyFile := flags.String("key", "", "file containing a base64 Ed25519 seed")
	trust := flags.String("trust", "", "reviewed trust-v2.json file")
	legacy := flags.String("legacy-roots", "", "optional JSON map of imported v1 key IDs to base64 public keys")
	publisher := flags.String("publisher", "", "authorized v2 key ID (migrate)")
	target := flags.String("id", "", "approved target plugin ID (migrate)")
	module := flags.String("module", "", "rebuilt WASI module (migrate)")
	host := flags.String("host", "", "reviewed OmniHive host version (migrate)")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if mode == "keygen" {
		if *keyFile == "" {
			return errors.New("keygen requires -key for the offline seed file")
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(*keyFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(f, base64.StdEncoding.EncodeToString(priv.Seed()))
		err = errors.Join(err, f.Close())
		if err != nil {
			_ = os.Remove(*keyFile)
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, base64.StdEncoding.EncodeToString(pub))
		return err
	}
	if (mode != "sign" && mode != "migrate") || *src == "" || *keyFile == "" || *trust == "" || filepath.Ext(*out) != ".ohplugin" {
		return errors.New("specify sign or migrate, -src, -trust, -key and a new .ohplugin -out")
	}
	seedText, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(seedText)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("invalid Ed25519 seed file")
	}
	private := ed25519.NewKeyFromSeed(seed)
	dir, err := os.MkdirTemp("", "ohplugin-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	policy, err := os.ReadFile(*trust)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "trust-v2.json"), policy, 0600); err != nil {
		return err
	}
	cfg := &config.Config{Plugins: config.PluginConfig{DataDir: dir, MaxUploadBytes: 128 << 20, MaxUncompressedBytes: 256 << 20}}
	if *legacy != "" {
		data, e := os.ReadFile(*legacy)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(data, &cfg.Plugins.TrustedPublishers); e != nil {
			return e
		}
	}
	installer := service.NewPluginPackageInstaller(cfg, service.PluginHostInfo{Version: *host})
	f, err := os.CreateTemp(filepath.Dir(*out), ".ohplugin-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	ctx := context.Background()
	switch mode {
	case "sign":
		root, e := os.OpenRoot(*src)
		if e != nil {
			return e
		}
		defer root.Close()
		in, e := root.Open("manifest.json")
		if e != nil {
			return e
		}
		defer in.Close()
		dec := json.NewDecoder(in)
		dec.DisallowUnknownFields()
		var m service.PluginManifest
		if e = dec.Decode(&m); e != nil {
			return e
		}
		if dec.Decode(&struct{}{}) != io.EOF {
			return errors.New("unexpected extra manifest data")
		}
		err = installer.WriteExtension(ctx, f, m, root, private)
	case "migrate":
		in, e := os.Open(*src)
		if e != nil {
			return e
		}
		defer in.Close()
		wasm, e := os.Open(*module)
		if e != nil {
			return e
		}
		defer wasm.Close()
		err = installer.RebuildV1(ctx, in, wasm, f, *target, *publisher, private)
	}
	if err != nil {
		return err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err = installer.Install(ctx, f, nil); err != nil {
		return fmt.Errorf("verify finished package: %w", err)
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Atomic publication with no overwrite; failures leave the old release intact.
	return os.Link(f.Name(), *out)
}
