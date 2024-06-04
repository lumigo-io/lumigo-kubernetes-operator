package internal

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

const (
	DEFAULT_JS_CLIENT_IMG_NAME = "host.docker.internal:5000/test-apps/js/client"
	DEFAULT_JS_SERVER_IMG_NAME = "host.docker.internal:5000/test-apps/js/server"
)

func BuildDockerImageAndExportArchive(imageName, sourceFolder, imageArchivePath string, logger *log.Logger) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		logger.Printf("Building '%s' image", imageName)

		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return ctx, fmt.Errorf("cannot create Docker client: %w", err)
		}
		defer cli.Close()

		tar, err := archive.TarWithOptions(sourceFolder, &archive.TarOptions{})
		if err != nil {
			return ctx, fmt.Errorf("cannot create tar archive for Docker build context: %w", err)
		}

		{
			res, err := cli.ImageBuild(ctx, tar, types.ImageBuildOptions{
				Dockerfile: "Dockerfile",
				Tags:       []string{imageName},
				Remove:     true,
			})
			if _, err1 := io.Copy(logger.Writer(), res.Body); err1 != nil {
				logger.Fatalf("cannot read ImageBuild output: %v", err1)
			}
			if err != nil {
				return ctx, fmt.Errorf("cannot build '%s' image: %w", imageName, err)
			}
			defer res.Body.Close()
			logger.Printf("Image '%s' built", imageName)
		}

		if imageArchivePath != "" {
			logger.Printf("Saving '%s' image to path '%s'", imageName, imageArchivePath)
			if err := saveDockerImageToPath(ctx, cli, imageName, imageArchivePath); err != nil {
				return ctx, fmt.Errorf("cannot save '%s' image to path '%s': %w", imageName, imageArchivePath, err)
			}

			logger.Printf("Image '%s' saved to path '%s'", imageName, imageArchivePath)
		}

		return ctx, nil
	}
}

func saveDockerImageToPath(ctx context.Context, cli *client.Client, imageName, imageArchivePath string) error {
	f, err := os.Create(imageArchivePath)
	if err != nil {
		return fmt.Errorf("cannot open file '%s': %w", imageArchivePath, err)
	}
	defer f.Close()

	r, err := cli.ImageSave(ctx, []string{imageName})
	if err != nil {
		return fmt.Errorf("cannot get reader for '%s' image: %w", imageName, err)
	}
	defer r.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("an error occurred while copying archive for '%s' image to '%s': %w", imageName, imageArchivePath, err)
	}

	return nil
}
