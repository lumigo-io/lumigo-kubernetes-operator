package kind

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/stdr"
	"github.com/lumigo-io/lumigo-kubernetes-operator/tests/quickstart/internal"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
)

var (
	testEnv env.Environment
)

const (
	DEFAULT_KIND_NODE_IMAGE = "kindest/node:v1.27.1"
)

func TestMain(m *testing.M) {
	cfg, _ := envconf.NewFromFlags()

	var kindClusterName string
	var runId string

	if ghRunId, isPresent := os.LookupEnv("GITHUB_RUN_ID"); isPresent {
		// Use the GitHub job id as run id, as it removes unnecessary cognitive
		// complexity in analyzing the OTLP data
		kindClusterName = fmt.Sprintf("lumigo-operator-%s", ghRunId)
		runId = ghRunId
	} else {
		kindClusterName = envconf.RandomName("lumigo-operator", 24)
		runId = kindClusterName[16:]
	}

	logger := log.New(os.Stderr, "", 0)

	isLumigoOperatorDebug := true
	if val, isPresent := os.LookupEnv("LUMIGO_DEBUG"); isPresent && val == "false" {
		isLumigoOperatorDebug = false
	}

	cwd, _ := os.Getwd()
	tmpDir := filepath.Join(cwd, "resources", "test-runs", runId)
	if err := createDir(tmpDir, os.ModePerm); err != nil {
		logger.Fatalf("Cannot create test-run tmp dir at '%s': %v", tmpDir, err)
	}

	kindNodeImageVal, isKindNodeImagePresent := os.LookupEnv("KIND_NODE_IMAGE")
	if !isKindNodeImagePresent {
		kindNodeImageVal = DEFAULT_KIND_NODE_IMAGE
	}

	kindConfigTemplatePath := filepath.Join(cwd, "resources", "kind-config.yaml.tpl")
	kindConfig, err := os.ReadFile(kindConfigTemplatePath)
	if err != nil {
		logger.Fatalf("Cannot read Kind config template at '%s': %v", kindConfigTemplatePath, err)
	}

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

	repoRoot := filepath.Dir(filepath.Dir(cwd))

	controllerImageName := fmt.Sprintf("%s:%s", internal.DEFAULT_CONTROLLER_IMG_NAME, runId)
	controllerImageArchivePath := filepath.Join(tmpDir, "operator-controller.tgz")

	telemetryProxyImageName := fmt.Sprintf("%s:%s", internal.DEFAULT_PROXY_IMG_NAME, runId)
	telemetryProxyImageArchivePath := filepath.Join(tmpDir, "telemetry-proxy-controller.tgz")

	quickstartNamespaces := []string{"quickstart-1", "quickstart-2", "quickstart-3"}
	lumigoNamespace := "lumigo-system"

	ctx := context.WithValue(context.Background(), internal.ContextKeyRunId, runId)
	ctx = context.WithValue(ctx, internal.ContextKeyKubernetesClusterName, kindClusterName)
	ctx = context.WithValue(ctx, internal.ContextKeyLumigoOperatorDebug, isLumigoOperatorDebug)
	ctx = context.WithValue(ctx, internal.ContextKeyOperatorControllerImage, controllerImageName)
	ctx = context.WithValue(ctx, internal.ContextKeyOperatorTelemetryProxyImage, telemetryProxyImageName)
	ctx = context.WithValue(ctx, internal.ContextKeyLumigoToken, "t_123456789012345678901")
	ctx = context.WithValue(ctx, internal.ContextQuickstartNamespaces, quickstartNamespaces)
	ctx = context.WithValue(ctx, internal.ContextKeyLumigoNamespace, lumigoNamespace)
	testEnv = env.NewWithConfig(cfg).WithContext(ctx)

	logrWrapper := stdr.New(logger)
	testEnv.Setup(
		internal.BuildDockerImageAndExportArchive(controllerImageName, filepath.Join(repoRoot, "controller"), controllerImageArchivePath, logger),
		internal.BuildDockerImageAndExportArchive(telemetryProxyImageName, filepath.Join(repoRoot, "telemetryproxy"), telemetryProxyImageArchivePath, logger),
		envfuncs.CreateKindClusterWithConfig(kindClusterName, kindNodeImageVal, kindConfigPath),
		internal.LoadDockerImageArchiveToCluster(kindClusterName, controllerImageArchivePath, logger),
		internal.LoadDockerImageArchiveToCluster(kindClusterName, telemetryProxyImageArchivePath, logger),

		// Create the namespace on which the quickstart feature will operate
		envfuncs.CreateNamespace(quickstartNamespaces[0]),
		envfuncs.CreateNamespace(quickstartNamespaces[1]),
		envfuncs.CreateNamespace(quickstartNamespaces[2]),

		internal.LumigoOperatorEnvFunc(lumigoNamespace, "http://nothing.real.com", logrWrapper, []string{
			fmt.Sprintf("--set monitoredNamespaces[0].namespace=%s", quickstartNamespaces[0]),
			fmt.Sprintf("--set monitoredNamespaces[0].loggingEnabled=%t", false),
			fmt.Sprintf("--set monitoredNamespaces[0].tracingEnabled=%t", false),
		}),
	)

	testEnv.Finish(
		envfuncs.ExportKindClusterLogs(kindClusterName, kindLogsDir),
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
