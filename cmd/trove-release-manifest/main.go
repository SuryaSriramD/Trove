// trove-release-manifest prepares unsigned, canonical signing input. It does
// not generate or access private keys, sign an artifact, or install anything.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"trove.local/phase0/internal/updates"
)

func bounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return nil, errors.New("configuration must be a bounded regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if int64(len(raw)) > limit {
		return nil, errors.New("configuration exceeds bound")
	}
	return raw, err
}
func run(args []string) error {
	flags := flag.NewFlagSet("trove-release-manifest", flag.ContinueOnError)
	artifact := flags.String("artifact", "", "Existing portable artifact directory")
	identity := flags.String("identity", "", "Explicit release metadata JSON")
	policyPath := flags.String("publisher-policy", "", "Trusted packaging public-key/certificate policy, never from the candidate")
	output := flags.String("output", "", "Unused canonical manifest output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *artifact == "" || *identity == "" || *policyPath == "" || *output == "" {
		return errors.New("artifact, identity, publisher-policy and new output are required")
	}
	if _, err := os.Lstat(*output); !os.IsNotExist(err) {
		return errors.New("manifest output must be a new path")
	}
	if updates.PathsOverlap(*artifact, *policyPath) {
		return errors.New("publisher policy must come from trusted packaging configuration outside the artifact")
	}
	policyRaw, err := bounded(*policyPath, 16<<10)
	if err != nil {
		return err
	}
	policy, err := updates.ParsePublisherPolicy(policyRaw)
	if err != nil {
		return err
	}
	raw, err := bounded(*identity, 16<<10)
	if err != nil {
		return err
	}
	var id updates.Identity
	if err = json.Unmarshal(raw, &id); err != nil {
		return err
	}
	if err = policy.AuthorizeIdentity(id); err != nil {
		return err
	}
	// Build outside the artifact or at its fixed manifest name only, so temporary
	// signing output cannot accidentally become a payload object on the next run.
	absolute, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(*artifact)
	if err != nil {
		return err
	}
	if updates.PathsOverlap(root, absolute) && absolute != filepath.Join(root, updates.ReleaseManifestName) {
		return errors.New("manifest output inside artifact must use its fixed authentication filename")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	manifest, err := updates.BuildReleaseManifest(ctx, root, id)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write(manifest); err != nil {
		return err
	}
	return file.Sync()
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "BLOCKED:", err)
		os.Exit(1)
	}
}
