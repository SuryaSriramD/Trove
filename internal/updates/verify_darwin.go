//go:build darwin

package updates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type MacVerifier struct{}

// Inspect uses Apple's signature requirement evaluator, then Gatekeeper policy.
// Ad-hoc builds have no TeamIdentifier and cannot authorize updates. No shell,
// private key access, signature rewriting or quarantine removal is involved.
func (MacVerifier) Inspect(ctx context.Context, path string) (Application, error) {
	var out Application
	path, err := filepath.Abs(path)
	if err != nil {
		return out, err
	}
	if filepath.Ext(path) != ".app" {
		return out, errors.New("choose a Trove application bundle")
	}
	before, err := InspectTree(ctx, path)
	if err != nil {
		return out, err
	}
	if _, err = runSystem(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", path); err != nil {
		return out, errors.New("application signature or sealed resources are invalid")
	}
	details, err := runSystem(ctx, "/usr/bin/codesign", "--display", "--verbose=4", path)
	if err != nil {
		return out, errors.New("application signing identity is unavailable")
	}
	for _, line := range strings.Split(string(details), "\n") {
		if strings.HasPrefix(line, "TeamIdentifier=") {
			out.Identity.TeamID = strings.TrimPrefix(line, "TeamIdentifier=")
		}
	}
	if !teamPattern.MatchString(out.Identity.TeamID) {
		return out, errors.New("verified updates require a Developer ID signed installation; this development build cannot authorize updates")
	}
	requirement := `anchor apple generic and identifier ` + strconv.Quote(BundleID) + ` and certificate 1[field.1.2.840.113635.100.6.2.6] exists and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = ` + strconv.Quote(out.Identity.TeamID)
	if _, err = runSystem(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", "-R="+requirement, path); err != nil {
		return out, errors.New("application is not a matching Apple Developer ID application")
	}
	if _, err = runSystem(ctx, "/usr/sbin/spctl", "--assess", "--type", "execute", path); err != nil {
		return out, errors.New("application did not pass macOS distribution policy")
	}
	metadata, err := runSystem(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", "--", filepath.Join(path, "Contents/Info.plist"))
	if err != nil {
		return out, errors.New("signed application metadata is unavailable")
	}
	var info struct {
		BundleID          string `json:"CFBundleIdentifier"`
		Version           string `json:"CFBundleShortVersionString"`
		Build             string `json:"CFBundleVersion"`
		UserVersion       int    `json:"TroveWorkspaceUserVersion"`
		RollbackHandshake int    `json:"TroveRollbackHandshakeVersion"`
		StorageGeneration int    `json:"TroveWorkspaceStorageGeneration"`
		Architecture      string `json:"TroveArchitecture"`
		MinimumOS         string `json:"LSMinimumSystemVersion"`
	}
	if json.Unmarshal(metadata, &info) != nil {
		return out, errors.New("signed application metadata is malformed")
	}
	out.Identity.BundleID = info.BundleID
	out.Identity.Version = info.Version
	out.Identity.UserVersion = info.UserVersion
	out.Identity.RollbackHandshake = info.RollbackHandshake
	out.Identity.StorageGeneration = info.StorageGeneration
	out.Identity.Architecture = info.Architecture
	out.Identity.MinimumOS = info.MinimumOS
	out.Identity.Build, err = parseBuild(info.Build)
	if err != nil {
		return out, err
	}
	out.Identity.Platform = "darwin"
	out.Identity.Publisher = Publisher{Kind: AppleDeveloperID, ID: out.Identity.TeamID}
	if err = validateIdentity(out.Identity); err != nil {
		return out, err
	}
	if info.Architecture != runtime.GOARCH {
		return out, errors.New("application does not match this Mac's architecture")
	}
	arch := "arm64"
	if info.Architecture == "amd64" {
		arch = "x86_64"
	}
	if _, err = runSystem(ctx, "/usr/bin/lipo", "-verify_arch", arch, filepath.Join(path, "Contents/MacOS/Trove")); err != nil {
		return out, errors.New("application executable does not match its signed architecture")
	}
	version, err := runSystem(ctx, "/usr/bin/sw_vers", "-productVersion")
	if err != nil || !compatibleOS(strings.TrimSpace(string(version)), info.MinimumOS) {
		return out, errors.New("application requires a newer macOS version")
	}
	after, err := InspectTree(ctx, path)
	if err != nil {
		return out, err
	}
	if before.SHA256 != after.SHA256 {
		return out, errors.New("application changed during verification")
	}
	out.Inventory = after
	return out, nil
}

type boundedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (64 << 10) - b.Len()
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func runSystem(ctx context.Context, binary string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C", "LC_ALL=C"}
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if output.overflow {
		return nil, errors.New("platform verification output exceeded its bound")
	}
	return output.Bytes(), nil
}
