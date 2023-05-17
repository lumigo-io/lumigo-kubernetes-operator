package kind

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lumigo-io/lumigo-kubernetes-operator/tests/kubernetes-distros/kind/internal"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
)

var (
	testEnv env.Environment
)

const (
	DEFAULT_KIND_NODE_IMAGE        = "kindest/node:v1.27.1"
	LUMIGO_SYSTEM_NAMESPACE        = "lumigo-system"
	OTLP_SINK_OTEL_COLLECTOR_IMAGE = "otel/opentelemetry-collector:0.76.1"
	OTLP_SINK_NAMESPACE            = "otlp-sink"
)

func TestMain(m *testing.M) {
	cfg, _ := envconf.NewFromFlags()

	var kindClusterName string
	var runId string

	if ghRunId, isPresent := os.LookupEnv("GITHUB_RUN_ID"); isPresent {
		// Use the GitHub job id as run id, as it removes unnecessary congnitive
		// complexity in analyzing the OTLP data
		kindClusterName = fmt.Sprintf("lumigo-operator-%s", ghRunId)
		runId = ghRunId
	} else {
		kindClusterName = envconf.RandomName("lumigo-operator", 24)
		runId = kindClusterName[16:]
	}

	logger := log.New(os.Stderr, "", 0)

	cwd, _ := os.Getwd()
	tmpDir := filepath.Join(cwd, "resources", "test-runs", runId)
	if err := createDir(tmpDir, os.ModePerm); err != nil {
		logger.Fatalf("Cannot create test-run tmp dir at '%s': %v", tmpDir, err)
	}

	kindNodeImageVal, isPresent := os.LookupEnv("KIND_NODE_IMAGE")
	if !isPresent {
		kindNodeImageVal = DEFAULT_KIND_NODE_IMAGE
	}

	imageVersionVal, isPresent := os.LookupEnv("IMG_VERSION")
	if !isPresent {
		imageVersionVal = internal.DEFAULT_IMG_VERSION
	}

	controllerImageName, isPresent := os.LookupEnv("CONTROLLER_IMG")
	if !isPresent {
		controllerImageName = fmt.Sprintf("%s:%s", internal.DEFAULT_CONTROLLER_IMG_NAME, imageVersionVal)
	}

	telemetryProxyImageName, isPresent := os.LookupEnv("PROXY_IMG")
	if !isPresent {
		telemetryProxyImageName = fmt.Sprintf("%s:%s", internal.DEFAULT_PROXY_IMG_NAME, imageVersionVal)
	}

	logger.Printf("Using controller image '%s' and telemetry-proxy image '%s'", controllerImageName, telemetryProxyImageName)

	dataSinkConfigDir := filepath.Join(cwd, "resources", "otlp-sink", "config")

	dataSinkDataDir := filepath.Join(tmpDir, "otlp-sink", "data")
	if err := createDir(dataSinkDataDir, os.ModePerm); err != nil {
		logger.Fatalf("Cannot create OTLP sink data directory at '%s': %v", dataSinkDataDir, err)
	}

	kindConfigTemplatePath := filepath.Join(cwd, "resources", "kind-config.yaml.tpl")
	kindConfigTemplate, err := os.ReadFile(kindConfigTemplatePath)
	if err != nil {
		logger.Fatalf("Cannot read Kind config template at '%s': %v", kindConfigTemplatePath, err)
	}

	kindConfig := strings.Replace(
		strings.Replace(
			string(kindConfigTemplate),
			"{{OTLP_SINK_CONFIG_VOLUME_PATH}}",
			dataSinkConfigDir,
			1,
		),
		"{{OTLP_SINK_DATA_VOLUME_PATH}}",
		dataSinkDataDir,
		1,
	)

	kindLogsDir := filepath.Join(tmpDir, "kind", "logs")
	if err := createDir(kindLogsDir, os.ModePerm); err != nil {
		logger.Fatalf("Cannot create Kind logs directory at '%s': %v", kindLogsDir, err)
	}

	kindConfigDir := filepath.Join(tmpDir, "kind", "config")
	if err := createDir(kindConfigDir, os.ModePerm); err != nil {
		logger.Fatalf("Cannot create Kind config directory at '%s': %v", kindConfigDir, err)
	}

	kindConfigPath := filepath.Join(kindConfigDir, fmt.Sprintf("kind-config-%s.yaml", runId))
	if err := os.WriteFile(kindConfigPath, []byte(kindConfig), 0644); err != nil {
		logger.Fatalf("Cannot serialize Kind config to '%s': %v", kindConfigPath, err)
	}

	logger.Printf("Running tests on cluster '%s' using '%s' Kind image", kindClusterName, kindNodeImageVal)

	lumigoToken, isPresent := os.LookupEnv("LUMIGO_TRACER_TOKEN")
	if !isPresent {
		lumigoToken = DEFAULT_LUMIGO_TOKEN
	}

	ctx := context.WithValue(context.Background(), internal.ContextKeyRunId, runId)
	ctx = context.WithValue(ctx, internal.ContextKeyOtlpSinkConfigPath, dataSinkConfigDir)
	ctx = context.WithValue(ctx, internal.ContextKeyOtlpSinkDataPath, dataSinkDataDir)
	ctx = context.WithValue(ctx, internal.ContextKeyLumigoToken, lumigoToken)
	ctx = context.WithValue(ctx, internal.ContextKeyOperatorControllerImage, controllerImageName)
	ctx = context.WithValue(ctx, internal.ContextKeyOperatorProxyImage, telemetryProxyImageName)

	testEnv = env.NewWithConfig(cfg).WithContext(ctx)

	var loadControllerImageFunc, loadProxyImageFunc env.Func

	if controllerImageArchive, isPresent := os.LookupEnv("CONTROLLER_IMG_ARCHIVE"); isPresent {
		controllerImageArchiveAbsPath, err := validatePath(controllerImageArchive)
		if err != nil {
			logger.Fatalf("Failed to resolve absolute URL for %s image from archive %s: %v", controllerImageName, controllerImageArchive, err)
		} else {
			loadControllerImageFunc = wrapLoadImageArchiveWithLogging(kindClusterName, controllerImageArchiveAbsPath, *logger)
		}
	} else {
		loadControllerImageFunc = wrapLoadImageWithLogging(kindClusterName, controllerImageName, *logger)
	}

	if telemetryProxyImageArchive, isPresent := os.LookupEnv("PROXY_IMG_ARCHIVE"); isPresent {
		telemetryProxyImageArchiveAbsPath, err := validatePath(telemetryProxyImageArchive)
		if err != nil {
			logger.Fatalf("Failed to resolve absolute URL for %s image from archive %s: %v", telemetryProxyImageName, telemetryProxyImageArchive, err)
		} else {
			loadProxyImageFunc = wrapLoadImageArchiveWithLogging(kindClusterName, telemetryProxyImageArchiveAbsPath, *logger)
		}
	} else {
		loadProxyImageFunc = wrapLoadImageWithLogging(kindClusterName, telemetryProxyImageName, *logger)
	}

	testEnv.Setup(
		envfuncs.CreateKindClusterWithConfig(kindClusterName, kindNodeImageVal, kindConfigPath),
		loadControllerImageFunc,
		loadProxyImageFunc,
		envfuncs.CreateNamespace(OTLP_SINK_NAMESPACE),
		envfuncs.CreateNamespace(LUMIGO_SYSTEM_NAMESPACE),
		/*
		 * Otel Collector image is on Docker hub, no need to pull it into Kind (pulling into Kind
		 * works only for local image, in the local Docker daemon).
		 */
		// envfuncs.LoadDockerImageToCluster(kindClusterName, OTLP_SINK_OTEL_COLLECTOR_IMAGE),
	)

	testEnv.Finish(
		envfuncs.ExportKindClusterLogs(kindClusterName, kindLogsDir),
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			keepDataFolder, isPresent := os.LookupEnv("KEEP_OTLP_DATA")
			if !isPresent || keepDataFolder != "true" {
				if err := os.Remove(tmpDir); err != nil {
					return ctx, err
				}
			}
			return ctx, nil
		},
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			keepKindCluster, isPresent := os.LookupEnv("KEEP_KIND_CLUSTER")
			if !isPresent || keepKindCluster != "true" {
				return envfuncs.DestroyKindCluster(kindClusterName)(ctx, cfg)
			}

			return ctx, nil
		},
	)

	os.Exit(testEnv.Run(m))
}

func createDir(path string, fileMode os.FileMode) error {
	if err := os.MkdirAll(path, fileMode); err != nil {
		return err
	}

	// Ensure umask does not interfere with the permissions
	return os.Chmod(path, fileMode)
}

func validatePath(relativePath string) (string, error) {
	cwd, _ := os.Getwd()

	absPath, err := filepath.Abs(filepath.Join(cwd, relativePath))
	if err != nil {
		return "", fmt.Errorf("Failed to resolve relative path '%s': %v", relativePath, err)
	} else if _, err := os.Stat(absPath); errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("The resolved absolute path '%s' does not exist", absPath)
	}

	return absPath, nil
}

func wrapLoadImageArchiveWithLogging(kindClusterName, containerImageArchivePath string, logger log.Logger) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		logger.Printf("Loading the image archive '%[2]s' into the Kind cluster '%[1]v'\n", kindClusterName, containerImageArchivePath)
		delegate := envfuncs.LoadImageArchiveToCluster(kindClusterName, containerImageArchivePath)
		return delegate(ctx, cfg)
	}
}

func wrapLoadImageWithLogging(kindClusterName, containerImageName string, logger log.Logger) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		logger.Printf("Loading the image '%[2]s' into the Kind cluster '%[1]v'\n", kindClusterName, containerImageName)
		delegate := envfuncs.LoadDockerImageToCluster(kindClusterName, containerImageName)
		return delegate(ctx, cfg)
	}
}
