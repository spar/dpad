package compose

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// maxSource bounds a Compose file read for editing.
const maxSource = 4 << 20

// ReadSource reads one of p's Compose files for editing, over SSH when the
// project runs on the SSH host.
func ReadSource(ctx context.Context, env []string, p Project, file string) ([]byte, error) {
	if err := editable(p, file); err != nil {
		return nil, err
	}
	if !p.Remote {
		return os.ReadFile(file)
	}
	out, err := runScript(ctx, env, "cat -- "+quote(file), nil)
	if err != nil {
		return nil, fmt.Errorf("read %s on the SSH host: %w", file, err)
	}
	return out, nil
}

// Check resolves p's configuration with data standing in for file, without
// touching file. The returned project describes the edited configuration.
func Check(ctx context.Context, env []string, p Project, file string, data []byte) (Project, error) {
	if err := editable(p, file); err != nil {
		return Project{}, err
	}
	candidate, remove, err := writeCandidate(ctx, env, p, file, data)
	if err != nil {
		return Project{}, err
	}
	defer remove()
	edited := p
	edited.Issue = ""
	edited.Files = slices.Clone(p.Files)
	edited.Files[slices.Index(p.Files, file)] = candidate
	if err := resolve(ctx, env, &edited); err != nil {
		return Project{}, err
	}
	edited.Files = p.Files
	return edited, nil
}

// Replace writes data over file through a temporary file in the same
// directory, so a failed write leaves the original as it was. It refuses when
// file no longer holds original, which means someone else changed it.
func Replace(ctx context.Context, env []string, p Project, file string, original, data []byte) error {
	if err := editable(p, file); err != nil {
		return err
	}
	if p.Remote {
		return replaceRemote(ctx, env, file, original, data)
	}
	target, err := filepath.EvalSymlinks(file)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return changedError(file)
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	temp, err := writeTemp(target, data, info.Mode().Perm())
	if err != nil {
		return err
	}
	if err := os.Rename(temp, target); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func changedError(file string) error {
	return fmt.Errorf("%s changed on disk while you were editing it; edit again to start from the new version", path.Base(filepath.ToSlash(file)))
}

// replaceStatus is the exit status of replaceScript when the file changed.
const replaceStatus = 3

// replaceScript follows a symlinked file, checks its hash, and renames a
// temporary file holding stdin over it with the original's mode. It sticks
// to tools GNU, BusyBox and BSD systems all have.
const replaceScript = `set -e
r=$(readlink -f -- "$f" 2>/dev/null) || r=$f
[ -n "$r" ] || r=$f
h=$(sha256sum -- "$r" 2>/dev/null || shasum -a 256 -- "$r")
if [ "${h%% *}" != "$want" ]; then exit 3; fi
m=$(stat -c %a -- "$r" 2>/dev/null || stat -f %Lp -- "$r")
t=$(mktemp "$(dirname -- "$r")/.$(basename -- "$r").dpad-XXXXXX")
trap 'rm -f -- "$t"' EXIT
cat > "$t"
chmod "$m" "$t"
mv -f -- "$t" "$r"
trap - EXIT`

func replaceRemote(ctx context.Context, env []string, file string, original, data []byte) error {
	sum := sha256.Sum256(original)
	script := "f=" + quote(file) + " want=" + hex.EncodeToString(sum[:]) + "\n" + replaceScript
	_, err := runScript(ctx, env, script, data)
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == replaceStatus {
		return changedError(file)
	}
	if err != nil {
		return fmt.Errorf("save %s on the SSH host: %w", file, err)
	}
	return nil
}

// writeCandidate puts data in a hidden file beside file, so relative paths
// and includes in it resolve as they do from file. remove deletes it.
func writeCandidate(ctx context.Context, env []string, p Project, file string, data []byte) (string, func(), error) {
	if !p.Remote {
		name, err := writeTemp(file, data, 0o600)
		return name, func() { _ = os.Remove(name) }, err
	}
	template := path.Dir(file) + "/." + path.Base(file) + ".dpad-XXXXXX"
	out, err := runScript(ctx, env, "umask 077 && t=$(mktemp "+quote(template)+") && cat > \"$t\" && printf %s \"$t\"", data)
	if err != nil {
		return "", nil, fmt.Errorf("write a copy beside %s on the SSH host: %w", file, err)
	}
	name := string(out)
	return name, func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_, _ = runScript(ctx, env, "rm -f -- "+quote(name), nil)
	}, nil
}

// runScript runs script on the SSH host with stdin as its input, returning
// its output, or its error output as the error.
func runScript(ctx context.Context, env []string, script string, stdin []byte) ([]byte, error) {
	cmd, err := sshScript(ctx, env, script, stdin != nil)
	if err != nil {
		return nil, err
	}
	var stdout, stderr limitedOutput
	stdout.limit, stderr.limit = maxSource, 8192
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return nil, fmt.Errorf("%s: %w", message, err)
		}
		return nil, err
	}
	if stdout.overflow {
		return nil, errors.New("file is larger than 4 MiB")
	}
	return stdout.data, nil
}

// Editable reports why p's Compose files cannot be edited, if they cannot. A
// project whose configuration is invalid can still be edited, to fix it.
func Editable(p Project) error {
	switch {
	case len(p.CLI) == 0 || len(p.Files) == 0:
		if p.Issue != "" {
			return errors.New(p.Issue)
		}
		return errors.New("the project's Compose configuration has not been read")
	}
	return nil
}

func editable(p Project, file string) error {
	if err := Editable(p); err != nil {
		return err
	}
	if !slices.Contains(p.Files, file) {
		return fmt.Errorf("%s is not one of the project's Compose files", file)
	}
	return nil
}

// writeTemp writes data to a hidden file beside another.
func writeTemp(beside string, data []byte, perm os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(beside), "."+filepath.Base(beside)+".dpad-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(name, perm)
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// Changes summarizes how an edit changes what the project runs, for the
// review before saving.
func Changes(before, after Project) []string {
	if before.Issue != "" {
		return []string{"The configuration did not resolve before this edit; it does now."}
	}
	var lines []string
	for _, name := range after.Services {
		image := after.ServiceImages[name]
		switch old := before.ServiceImages[name]; {
		case !slices.Contains(before.Services, name):
			lines = append(lines, name+": added"+imageSuffix(image))
		case old != image:
			lines = append(lines, name+": "+orNone(old)+" → "+orNone(image))
		}
	}
	for _, name := range before.Services {
		if !slices.Contains(after.Services, name) {
			lines = append(lines, name+": removed")
		}
	}
	switch {
	case len(lines) > 0:
	case before.ConfigHash != after.ConfigHash:
		lines = append(lines, "Service settings change; images stay the same.")
	default:
		lines = append(lines, "The resolved configuration is the same (comments or formatting only).")
	}
	return lines
}

func imageSuffix(image string) string {
	if image == "" {
		return ""
	}
	return " (" + image + ")"
}

func orNone(image string) string {
	if image == "" {
		return "no image"
	}
	return image
}
