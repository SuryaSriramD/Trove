package updates

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

const BundleID = "local.trove.desktop"

type Identity struct {
	BundleID          string `json:"bundleId"`
	TeamID            string `json:"teamId"`
	Version           string `json:"version"`
	Build             uint64 `json:"build"`
	UserVersion       int    `json:"userVersion"`
	StorageGeneration int    `json:"storageGeneration"`
	Architecture      string `json:"architecture"`
	MinimumOS         string `json:"minimumOS"`
}

type Application struct {
	Identity  Identity  `json:"identity"`
	Inventory Inventory `json:"inventory"`
}

type Verifier interface {
	Inspect(context.Context, string) (Application, error)
}

var teamPattern = regexp.MustCompile(`^[A-Z0-9]{10}$`)
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func validateIdentity(id Identity) error {
	if id.BundleID != BundleID || !teamPattern.MatchString(id.TeamID) || !versionPattern.MatchString(id.Version) || id.Build == 0 || id.UserVersion < 3 || id.StorageGeneration < 1 || (id.Architecture != "arm64" && id.Architecture != "amd64") || !osVersionPattern.MatchString(id.MinimumOS) {
		return errors.New("application lacks supported signed update metadata")
	}
	return nil
}

func VerifyUpgrade(current, next Application) error {
	if err := validateIdentity(current.Identity); err != nil {
		return err
	}
	if err := validateIdentity(next.Identity); err != nil {
		return err
	}
	if current.Identity.BundleID != next.Identity.BundleID || current.Identity.TeamID != next.Identity.TeamID {
		return errors.New("update publisher differs from this application")
	}
	if current.Identity.Architecture != next.Identity.Architecture {
		return errors.New("update architecture differs from this application")
	}
	if next.Identity.Build <= current.Identity.Build {
		return errors.New("update must have a newer signed build number")
	}
	if next.Identity.UserVersion < current.Identity.UserVersion || next.Identity.StorageGeneration < current.Identity.StorageGeneration {
		return errors.New("update would downgrade the workspace format; use paired rollback instead")
	}
	if !validDigest(current.Inventory.SHA256) || !validDigest(next.Inventory.SHA256) || current.Inventory.SHA256 == next.Inventory.SHA256 {
		return errors.New("update application identity is invalid")
	}
	return nil
}

var osVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+(?:\.[0-9]+)?$`)

func compatibleOS(current, minimum string) bool {
	if !osVersionPattern.MatchString(current) || !osVersionPattern.MatchString(minimum) {
		return false
	}
	var a, b [3]uint64
	for i, v := range strings.Split(current, ".") {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return false
		}
		a[i] = n
	}
	for i, v := range strings.Split(minimum, ".") {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return false
		}
		b[i] = n
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}

func parseBuild(value string) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != value {
		return 0, errors.New("signed application build number must be a positive integer")
	}
	return n, nil
}
