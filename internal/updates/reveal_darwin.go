//go:build darwin

package updates

import "context"

// Reveal opens Finder, never the application or an arbitrary shell command.
func Reveal(ctx context.Context, path string) error {
	_, err := runSystem(ctx, "/usr/bin/open", "-R", path)
	return err
}
