package internal

import (
	"context"
	"log"

	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
)

func LoadDockerImageArchiveToCluster(kindClusterName, containerImageArchivePath string, logger *log.Logger) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		logger.Printf("Loading the image archive '%[2]s' into the Kind cluster '%[1]v'\n", kindClusterName, containerImageArchivePath)
		delegate := envfuncs.LoadImageArchiveToCluster(kindClusterName, containerImageArchivePath)
		return delegate(ctx, cfg)
	}
}

func LoadDockerImageByNameToCluster(kindClusterName, containerImageName string, logger *log.Logger) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		logger.Printf("Loading the image '%[2]s' into the Kind cluster '%[1]v'\n", kindClusterName, containerImageName)
		delegate := envfuncs.LoadDockerImageToCluster(kindClusterName, containerImageName)
		return delegate(ctx, cfg)
	}
}
