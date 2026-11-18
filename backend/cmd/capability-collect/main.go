// capability-collect emits price-free unapproved observations on stdout. It has
// no publishing, database, account, or scheduling dependencies.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/capabilitysources"
)

func run() error {
	source := flag.String("source", "", "models.dev, openrouter, or litellm")
	input := flag.String("input", "", "local fixture; omit only for explicit network collection")
	verifiedAt := flag.String("verified-at", "", "fixture evidence date YYYY-MM-DD")
	network := flag.Bool("network", false, "explicitly fetch the approved public endpoint")
	flag.Parse()
	if _, err := capabilitysources.Endpoint(*source); err != nil {
		return err
	}
	var observation capabilitysources.Observation
	if *input != "" {
		if *network {
			return fmt.Errorf("input and network are mutually exclusive")
		}
		if _, err := time.Parse(time.DateOnly, *verifiedAt); err != nil {
			return err
		}
		file, err := os.Open(*input)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		body, err := io.ReadAll(io.LimitReader(file, capabilitysources.MaxBodyBytes+1))
		if err != nil {
			return err
		}
		observation, err = capabilitysources.Parse(*source, body, *verifiedAt)
		if err != nil {
			return err
		}
	} else {
		if !*network {
			return fmt.Errorf("require local input or explicit network flag")
		}
		capture, err := capabilitysources.Collect(context.Background(), nil, *source, nil)
		if err != nil {
			return err
		}
		observation = capture.Observation
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"schema_version": 1, "sources": []capabilitysources.Observation{observation}})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
