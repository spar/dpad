package compose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

var errRemoteEdit = errors.New("this project's Compose files are on the SSH host; dpad can only edit local Compose files for now")

// ReadSource reads one of p's Compose files for editing.
func ReadSource(p Project, file string) ([]byte, error) {
	if err := editable(p, file); err != nil {
		return nil, err
	}
	return os.ReadFile(file)
}

// Check resolves p's configuration with data standing in for file, without
// touching file. The returned project describes the edited configuration.
func Check(ctx context.Context, env []string, p Project, file string, data []byte) (Project, error) {
	if err := editable(p, file); err != nil {
		return Project{}, err
	}
	candidate, err := writeTemp(file, data, 0o600)
	if err != nil {
		return Project{}, err
	}
	defer os.Remove(candidate)
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
func Replace(p Project, file string, original, data []byte) error {
	if err := editable(p, file); err != nil {
		return err
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
		return fmt.Errorf("%s changed on disk while you were editing it; edit again to start from the new version", filepath.Base(file))
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

// Editable reports why p's Compose files cannot be edited, if they cannot. A
// project whose configuration is invalid can still be edited, to fix it.
func Editable(p Project) error {
	switch {
	case p.Remote:
		return errRemoteEdit
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

// writeTemp writes data to a hidden file beside path, so relative paths and
// includes in it resolve as they do from path.
func writeTemp(path string, data []byte, perm os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".dpad-*")
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
