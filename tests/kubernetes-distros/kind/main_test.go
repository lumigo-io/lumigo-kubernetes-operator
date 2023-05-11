package kind

import (
	"context"
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

	if ghJobId, isPresent := os.LookupEnv("GITHUB_JOB"); isPresent {
		// Use the GitHub job id as run id, as it removes unnecessary congnitive
		// complexity in analyzing the OTLP data
		kindClusterName = fmt.Sprintf("lumigo-operator-%s", ghJobId)
		runId = ghJobId
	} else {
		kindClusterName = envconf.RandomName("lumigo-operator", 24)
		runId = kindClusterName[16:]
	}

	logger := log.New(os.Stderr, "", 0)

	cwd, _ := os.Getwd()
	tmpDir := filepath.Join(cwd, "resources", "test-runs", runId)
	if err := os.MkdirAll(tmpDir, os.ModePerm); err != nil {
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
	if err := os.MkdirAll(dataSinkDataDir, os.ModePerm); err != nil {
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

	kindConfigDir := filepath.Join(tmpDir, "kind", "config")
	if err := os.MkdirAll(kindConfigDir, os.ModePerm); err != nil {
		logger.Fatalf("Cannot create Kind config directory at '%s': %v", kindConfigDir, err)
	}

	kindConfigPath := filepath.Join(kindConfigDir, fmt.Sprintf("kind-config-%s.yaml", runId))
	if err := os.WriteFile(kindConfigPath, []byte(kindConfig), 0644); err != nil {
		logger.Fatalf("Cannot serialize Kind config to '%s': %v", kindConfigPath, err)
	}

	logger.Println(fmt.Sprintf("Running tests on cluster '%s' using '%s' Kind image", kindClusterName, kindNodeImageVal), "kind-config", kindConfig)

	ctx := context.WithValue(context.Background(), internal.ContextKeyRunId, runId)
	ctx = context.WithValue(ctx, internal.ContextKeyOtlpSinkConfigPath, dataSinkConfigDir)
	ctx = context.WithValue(ctx, internal.ContextKeyOtlpSinkDataPath, dataSinkDataDir)

	testEnv = env.NewWithConfig(cfg).WithContext(ctx)

	testEnv.Setup(
		envfuncs.CreateKindClusterWithConfig(kindClusterName, kindNodeImageVal, kindConfigPath),
		envfuncs.CreateNamespace(OTLP_SINK_NAMESPACE),
		envfuncs.CreateNamespace(LUMIGO_SYSTEM_NAMESPACE),
		wrapLoadImageWithLogging(kindClusterName, controllerImageName, *logger),
		wrapLoadImageWithLogging(kindClusterName, telemetryProxyImageName, *logger),
		/*
		 * Otel Collector image is on Docker hub, no need to pull it into Kind (pulling into Kind
		 * works only for local image, in the local Docker daemon).
		 */
		// envfuncs.LoadDockerImageToCluster(kindClusterName, OTLP_SINK_OTEL_COLLECTOR_IMAGE),
	)

	testEnv.Finish(
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

func wrapLoadImageWithLogging(kindClusterName, containerImageName string, logger log.Logger) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		logger.Printf("Loading into the Kind cluster '%v' the image '%s'\n", kindClusterName, containerImageName)
		delegate := envfuncs.LoadDockerImageToCluster(kindClusterName, containerImageName)
		return delegate(ctx, cfg)
	}
}
