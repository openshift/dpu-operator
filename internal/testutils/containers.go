package testutils

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ContainerImage struct {
	Registry string
	Name     string
	Tag      string
}

func (ci ContainerImage) FullRef() string {
	return fmt.Sprintf("%s/%s:%s", ci.Registry, ci.Name, ci.Tag)
}

func (ci ContainerImage) LocalRef() string {
	return fmt.Sprintf("%s:%s", ci.Name, ci.Tag)
}

func executeCommand(ctx context.Context, binary string, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, append([]string{command}, args...)...)
	fullCommand := strings.Join(cmd.Args, " ")
	fmt.Println(fullCommand)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("command '%s' failed: %w: %s", fullCommand, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func executePodmanCommand(ctx context.Context, command string, args ...string) (string, error) {
	return executeCommand(ctx, "podman", command, args...)
}

func executeBuildahCommand(ctx context.Context, command string, args ...string) (string, error) {
	return executeCommand(ctx, "buildah", command, args...)
}

func BuildContainer(ctx context.Context, dockerfilePath string, tag string) error {
	_, err := executePodmanCommand(ctx, "build", "-f", dockerfilePath, "-t", tag, ".")
	return err
}

func PushContainer(ctx context.Context, imageRef string) error {
	_, err := executePodmanCommand(ctx, "push", imageRef)
	return err
}

func PullContainer(ctx context.Context, imageRef string, arch string, tag string) error {
	_, err := executePodmanCommand(ctx, "pull", fmt.Sprintf("--platform=%s", arch), imageRef)
	if err != nil {
		return err
	}
	_, err = executePodmanCommand(ctx, "tag", imageRef, tag)
	if err != nil {
		return err
	}
	return err
}

func RemoteImageExists(ctx context.Context, image ContainerImage) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err := executePodmanCommand(ctx, "manifest", "inspect", image.FullRef())
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "manifest unknown") {
		return false, nil
	}
	return false, err
}

func BuildAndPush(ctx context.Context, dockerfilePath string, image ContainerImage) error {
	if err := BuildContainer(ctx, dockerfilePath, image.LocalRef()); err != nil {
		return fmt.Errorf("failed to build container: %w", err)
	}

	fullRef := image.FullRef()
	if err := PushContainer(ctx, fullRef); err != nil {
		return fmt.Errorf("failed to push container: %w", err)
	}

	return nil
}

func EnsurePullAndPush(ctx context.Context, sourceImage, targetImage ContainerImage) error {
	sourceRef := sourceImage.FullRef()
	targetRef := targetImage.FullRef()

	localArm64ImageName := "tmp-manifest-arm64-" + strings.ReplaceAll(strings.ReplaceAll(sourceRef, "/", "-"), ":", "-")
	localAmd64ImageName := "tmp-manifest-amd64-" + strings.ReplaceAll(strings.ReplaceAll(sourceRef, "/", "-"), ":", "-")
	manifestName := "tmp-local-manifest-" + strings.ReplaceAll(strings.ReplaceAll(targetRef, "/", "-"), ":", "-")

	defer func() {
		if _, err := executeBuildahCommand(ctx, "manifest", "exists", manifestName); err == nil {
			if _, err := executeBuildahCommand(ctx, "manifest", "rm", manifestName); err != nil {
				fmt.Printf("Warning: failed to remove manifest %s: %v\n", manifestName, err)
			}
		}
	}()

	if err := PullContainer(ctx, sourceRef, "linux/arm64", localArm64ImageName); err != nil {
		return fmt.Errorf("failed to pull source image for linux/arm64 (%s) as %s: %w", sourceRef, localArm64ImageName, err)
	}
	if err := PullContainer(ctx, sourceRef, "linux/amd64", localAmd64ImageName); err != nil {
		return fmt.Errorf("failed to pull source image for linux/amd64 (%s) as %s: %w", sourceRef, localAmd64ImageName, err)
	}
	_, err := executeBuildahCommand(ctx, "manifest", "create", manifestName)
	if err != nil {
		return err
	}
	_, err = executeBuildahCommand(ctx, "manifest", "add", manifestName, localArm64ImageName)
	if err != nil {
		return err
	}
	_, err = executeBuildahCommand(ctx, "manifest", "add", manifestName, localAmd64ImageName)
	if err != nil {
		return err
	}
	_, err = executeBuildahCommand(ctx, "manifest", "push", "--all", manifestName, fmt.Sprintf("docker://%s", targetRef))
	if err != nil {
		return err
	}
	return nil
}
