package refactor

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

const releaseUsage = "Usage: omnihivectl release check --target VERSION --manifest PATH --checksums PATH --json [--archive PATH] [--fresh]\nExisting installations require OMNIHIVE_RELEASE_DSN."

// runRelease also works for container preflight: provide the target manifest and
// checksums from the release, and OMNIHIVE_RELEASE_DSN for the deployed database.
func runRelease(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runReleaseWithDB(ctx, args, stdout, stderr, openDB)
}

func runReleaseWithDB(ctx context.Context, args []string, stdout, stderr io.Writer, connect func(string) (*sql.DB, error)) int {
	if len(args) == 2 && args[0] == "check" && args[1] == "--help" {
		fmt.Fprintln(stdout, releaseUsage)
		return 0
	}
	if len(args) == 0 || args[0] != "check" {
		fmt.Fprintln(stderr, releaseUsage)
		return 2
	}
	f := flag.NewFlagSet("release check", flag.ContinueOnError)
	f.SetOutput(stderr)
	target := f.String("target", "", "target release")
	manifest := f.String("manifest", "", "release manifest")
	checksums := f.String("checksums", "", "release checksums")
	archive := f.String("archive", "", "optional release archive to verify")
	fresh := f.Bool("fresh", false, "no existing installation or database")
	f.Bool("json", false, "JSON output")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 || *target == "" || *manifest == "" || *checksums == "" {
		fmt.Fprintln(stderr, releaseUsage)
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	floor := ""
	if !*fresh {
		dsn := os.Getenv("OMNIHIVE_RELEASE_DSN")
		if dsn == "" {
			return releaseCheckError(stdout, stderr, *target, "OMNIHIVE_RELEASE_DSN is required for an existing installation (legacy mode also requires DB access)")
		}
		db, err := connect(dsn)
		if err != nil {
			return releaseCheckError(stdout, stderr, *target, "database rollback floor is unavailable")
		}
		defer db.Close()
		if err := db.QueryRowContext(ctx, "SELECT minimum_rollback_version FROM release_schema_compatibility WHERE singleton").Scan(&floor); err != nil {
			return releaseCheckError(stdout, stderr, *target, "database rollback floor is unavailable")
		}
	}
	data, err := os.ReadFile(*checksums)
	if err != nil {
		return releaseCheckError(stdout, stderr, *target, "cannot read checksums")
	}
	hash, err := service.ReleaseChecksum(data, filepath.Base(*manifest))
	if err != nil {
		return releaseCheckError(stdout, stderr, *target, err.Error())
	}
	result, err := service.CheckReleaseManifest(*manifest, hash, *target, floor, *fresh)
	if err == nil && *archive != "" {
		err = service.CheckReleaseArchive(*archive, *target, data)
		if err != nil {
			result.Eligible = false
		}
	}
	if err != nil && result.ExclusionReason == "" {
		result.ExclusionReason = err.Error()
	}
	if encodeErr := json.NewEncoder(stdout).Encode(result); encodeErr != nil {
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func releaseCheckError(stdout, stderr io.Writer, target, reason string) int {
	_ = json.NewEncoder(stdout).Encode(service.ReleaseEligibility{Target: target, ExclusionReason: reason})
	fmt.Fprintln(stderr, reason)
	return 1
}
