package kind

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-logr/stdr"
	gomplate "github.com/hairyhenderson/gomplate/v3"
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
	OTLP_SINK_OTEL_COLLECTOR_IMAGE = "otel/opentelemetry-collector:0.102.0"
	OTLP_SINK_NAMESPACE            = "otlp-sink"
)

type otlpSinkConfigDatasource struct {
	LumigoEndpoint string `json:"lumigo_endpoint"`
	LumigoToken    string `json:"lumigo_token"`
}

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

	lumigoEndpoint, isLumigoEndpointPresent := os.LookupEnv("LUMIGO_ENDPOINT")
	if !isLumigoEndpointPresent {
		lumigoEndpoint = internal.DEFAULT_LUMIGO_ENDPOINT
	}
	logger.Printf("Lumigo endpoint: %s", lumigoEndpoint)

	isLumigoOperatorDebug := true
	if val, isPresent := os.LookupEnv("LUMIGO_DEBUG"); isPresent && val == "false" {
		isLumigoOperatorDebug = false
	}

	lumigoTokenDatasourceValue := ""
	lumigoToken, isLumigoTokenPresent := os.LookupEnv("LUMIGO_TRACER_TOKEN")
	if !isLumigoTokenPresent {
		lumigoToken = DEFAULT_LUMIGO_TOKEN
	} else {
		lumigoTokenDatasourceValue = lumigoToken
	}
	logger.Printf("Lumigo token: %s", lumigoToken)

	cwd, _ := os.Getwd()
	tmpDir := filepath.Join(cwd, "resources", "test-runs", runId)
	if err := createDir(tmpDir, os.ModePerm); err != nil {
		logger.Fatalf("Cannot create test-run tmp dir at '%s': %v", tmpDir, err)
	}

	kindNodeImageVal, isKindNodeImagePresent := os.LookupEnv("KIND_NODE_IMAGE")
	if !isKindNodeImagePresent {
		kindNodeImageVal = DEFAULT_KIND_NODE_IMAGE
	}

	dataSinkConfigDir := filepath.Join(tmpDir, "otlp-sink", "config")
	if err := createDir(dataSinkConfigDir, os.ModePerm); err != nil {
		logger.Fatalf("Cannot create OTLP sink config directory at '%s': %v", dataSinkConfigDir, err)
	}

	dataSinkDataDir := filepath.Join(tmpDir, "otlp-sink", "data")
	if err := createDir(dataSinkDataDir, os.ModePerm); err != nil {
		logger.Fatalf("Cannot create OTLP sink data directory at '%s': %v", dataSinkDataDir, err)
	}

	otlpSinkConfigDatasourceFilePath := filepath.Join(dataSinkConfigDir, "config-datasource.json")
	otlpSinkConfigDatasourceFile, err := os.Create(otlpSinkConfigDatasourceFilePath)
	if err != nil {
		logger.Fatalf("Cannot create OTLP sink config datasource file at '%s': %v", otlpSinkConfigDatasourceFilePath, err)
	}

	if otlpSinkConfigDatasource, err := json.Marshal(otlpSinkConfigDatasource{
		LumigoEndpoint: lumigoEndpoint,
		LumigoToken:    lumigoTokenDatasourceValue,
	}); err != nil {
		logger.Fatalf("Cannot create OTLP sink config datasource file at '%s': %v", otlpSinkConfigDatasourceFilePath, err)
	} else {
		fmt.Fprint(otlpSinkConfigDatasourceFile, string(otlpSinkConfigDatasource))
	}

	otlpSinkConfigDatasourceFileUrl, _ := url.Parse("file://" + otlpSinkConfigDatasourceFilePath)
	renderer := gomplate.NewRenderer(gomplate.Options{
		Datasources: map[string]gomplate.Datasource{
			"config": {
				URL: otlpSinkConfigDatasourceFileUrl,
			},
		},
	})

	otlpSinkConfigTemplateFilePath := filepath.Join(cwd, "resources", "otlp-sink", "config.yaml.tpl")
	otlpSinkConfigTemplateText, err := os.ReadFile(otlpSinkConfigTemplateFilePath)
	if err != nil {
		logger.Fatalf("Cannot read the OTLP sink config template at '%s': %v", otlpSinkConfigTemplateFilePath, err)
	}

	otlpSinkConfigFilePath := filepath.Join(dataSinkConfigDir, "config.yaml")
	otlpSinkConfigFile, err := os.Create(otlpSinkConfigFilePath)
	if err != nil {
		logger.Fatalf("Cannot create OTLP sink config file at '%s': %v", otlpSinkConfigFilePath, err)
	}

	otlpSinkConfigTemplate := gomplate.Template{
		Name:   "OTLP sink configuration",
		Text:   string(otlpSinkConfigTemplateText),
		Writer: otlpSinkConfigFile,
	}

	if err := renderer.RenderTemplates(context.TODO(), []gomplate.Template{otlpSinkConfigTemplate}); err != nil {
		logger.Fatalf("Cannot render with gomplate the OTLP sink config file at '%s': %v", otlpSinkConfigFilePath, err)
	}

	otlpSinkConfigFile.Close()

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

	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(cwd)))

	controllerImageName := fmt.Sprintf("%s:%s", internal.DEFAULT_CONTROLLER_IMG_NAME, runId)
	controllerImageArchivePath := filepath.Join(tmpDir, "operator-controller.tgz")

	telemetryProxyImageName := fmt.Sprintf("%s:%s", internal.DEFAULT_PROXY_IMG_NAME, runId)
	telemetryProxyImageArchivePath := filepath.Join(tmpDir, "telemetry-proxy-controller.tgz")

	testJsClientImageName := fmt.Sprintf("%s:%s", internal.DEFAULT_JS_CLIENT_IMG_NAME, runId)
	testJsClientImageArchivePath := filepath.Join(tmpDir, "test-js-client.tgz")

	testJsServerImageName := fmt.Sprintf("%s:%s", internal.DEFAULT_JS_SERVER_IMG_NAME, runId)
	testJsServerImageArchivePath := filepath.Join(tmpDir, "test-js-server.tgz")

	testPythonImageName := fmt.Sprintf("%s:%s", internal.DEFAULT_PYTHON_IMG_NAME, runId)
	testPythonImageArchivePath := filepath.Join(tmpDir, "test-python.tgz")

	ctx := context.WithValue(context.Background(), internal.ContextKeyRunId, runId)
	ctx = context.WithValue(ctx, internal.ContextKeyKubernetesClusterName, kindClusterName)
	ctx = context.WithValue(ctx, internal.ContextKeyOtlpSinkConfigPath, dataSinkConfigDir)
	ctx = context.WithValue(ctx, internal.ContextKeyOtlpSinkDataPath, dataSinkDataDir)
	ctx = context.WithValue(ctx, internal.ContextKeySendDataToLumigo, isLumigoTokenPresent)
	ctx = context.WithValue(ctx, internal.ContextKeyLumigoEndpoint, lumigoEndpoint)
	ctx = context.WithValue(ctx, internal.ContextKeyLumigoOperatorDebug, isLumigoOperatorDebug)
	ctx = context.WithValue(ctx, internal.ContextKeyLumigoToken, lumigoToken)
	ctx = context.WithValue(ctx, internal.ContextKeyOperatorControllerImage, controllerImageName)
	ctx = context.WithValue(ctx, internal.ContextKeyOperatorTelemetryProxyImage, telemetryProxyImageName)
	ctx = context.WithValue(ctx, internal.ContextTestAppJsClientImageName, testJsClientImageName)
	ctx = context.WithValue(ctx, internal.ContextTestAppJsServerImageName, testJsServerImageName)
	ctx = context.WithValue(ctx, internal.ContextTestAppPythonImageName, testPythonImageName)
	ctx = context.WithValue(ctx, internal.ContextTestAppBusyboxIncludedContainerNamePrefix, "busybox-included")
	ctx = context.WithValue(ctx, internal.ContextTestAppBusyboxExcludedContainerNamePrefix, "busybox-excluded")
	ctx = context.WithValue(ctx, internal.ContextTestAppNamespacePrefix, "test-logs-ns")

	testEnv = env.NewWithConfig(cfg).WithContext(ctx)

	logrWrapper := stdr.New(logger)
	otlpSinkFeature, otlpSinkK8sServiceUrl := internal.OtlpSinkEnvFunc(OTLP_SINK_NAMESPACE, "otlp-sink", OTLP_SINK_OTEL_COLLECTOR_IMAGE, logrWrapper)
	lumigoOperatorFeature := internal.LumigoOperatorEnvFunc(LUMIGO_SYSTEM_NAMESPACE, otlpSinkK8sServiceUrl, logrWrapper)

	testEnv.Setup(
		internal.BuildDockerImageAndExportArchive(controllerImageName, filepath.Join(repoRoot, "controller"), controllerImageArchivePath, logger),
		internal.BuildDockerImageAndExportArchive(telemetryProxyImageName, filepath.Join(repoRoot, "telemetryproxy"), telemetryProxyImageArchivePath, logger),
		internal.BuildDockerImageAndExportArchive(testJsClientImageName, filepath.Join(cwd, "apps", "client"), testJsClientImageArchivePath, logger),
		internal.BuildDockerImageAndExportArchive(testJsServerImageName, filepath.Join(cwd, "apps", "server"), testJsServerImageArchivePath, logger),
		internal.BuildDockerImageAndExportArchive(testPythonImageName, filepath.Join(cwd, "apps", "python"), testPythonImageArchivePath, logger),

		envfuncs.CreateKindClusterWithConfig(kindClusterName, kindNodeImageVal, kindConfigPath),

		internal.LoadDockerImageArchiveToCluster(kindClusterName, controllerImageArchivePath, logger),
		internal.LoadDockerImageArchiveToCluster(kindClusterName, telemetryProxyImageArchivePath, logger),
		internal.LoadDockerImageArchiveToCluster(kindClusterName, testJsClientImageArchivePath, logger),
		internal.LoadDockerImageArchiveToCluster(kindClusterName, testJsServerImageArchivePath, logger),
		internal.LoadDockerImageArchiveToCluster(kindClusterName, testPythonImageArchivePath, logger),

		/*
		 * Otel Collector image is on Docker hub, no need to pull it into Kind (pulling into Kind
		 * works only for local image, in the local Docker daemon).
		 */
		envfuncs.CreateNamespace(OTLP_SINK_NAMESPACE),
		otlpSinkFeature,
		lumigoOperatorFeature,
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
