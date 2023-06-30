package telemetryproxyconfigs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/go-logr/logr"
)

type NamespaceMonitoringConfig struct {
	Token string `json:"token"`
	Name  string `json:"name"`
	Uid   string `json:"uid"`
}

func RemoveTelemetryProxyMonitoringOfNamespace(ctx context.Context, telemetryProxyNamespaceConfigurationsPath string, namespaceName string, log *logr.Logger) (bool, error) {
	return updateTelemetryProxyMonitoringOfNamespace(ctx, telemetryProxyNamespaceConfigurationsPath, &NamespaceMonitoringConfig{
		Name: namespaceName,
	}, log)
}

func UpsertTelemetryProxyMonitoringOfNamespace(ctx context.Context, telemetryProxyNamespaceConfigurationsPath string, namespaceName string, namespaceUid string, token string, log *logr.Logger) (bool, error) {
	return updateTelemetryProxyMonitoringOfNamespace(ctx, telemetryProxyNamespaceConfigurationsPath, &NamespaceMonitoringConfig{
		Name:  namespaceName,
		Uid:   namespaceUid,
		Token: token,
	}, log)
}

func updateTelemetryProxyMonitoringOfNamespace(ctx context.Context, telemetryProxyNamespaceConfigurationsPath string, namespaceMonitoringConfig *NamespaceMonitoringConfig, log *logr.Logger) (bool, error) {
	upsert := len(namespaceMonitoringConfig.Uid) > 0

	var namespaces []NamespaceMonitoringConfig
	namespacesFileBytes, err := os.ReadFile(telemetryProxyNamespaceConfigurationsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("cannot read namespace configuration file '%s': %w", telemetryProxyNamespaceConfigurationsPath, err)
		}
	} else if err := json.Unmarshal(namespacesFileBytes, &namespaces); err != nil {
		return false, fmt.Errorf("cannot unmarshal namespace configuration file '%s': %w", telemetryProxyNamespaceConfigurationsPath, err)
	}

	var newNamespaces []NamespaceMonitoringConfig
	// Keep all other namespaces to the new file
	for _, namespace := range namespaces {
		if namespace.Name != namespaceMonitoringConfig.Name && len(namespaceMonitoringConfig.Name) > 0 {
			newNamespaces = append(newNamespaces, namespace)
		}
	}

	if upsert {
		newNamespaces = append(newNamespaces, *namespaceMonitoringConfig)
	}

	// Sort namespace structs by namespace name
	sort.Slice(newNamespaces, func(i, j int) bool {
		return newNamespaces[i].Name < newNamespaces[j].Name
	})

	// The marhsalling is with sorted keys, so the resulting bytes are deterministic
	updatedNamespacesFileBytes, err := json.Marshal(newNamespaces)
	if err != nil {
		return false, fmt.Errorf("cannot marshal the updated namespace configuration: %w", err)
	}

	if bytes.Equal(namespacesFileBytes, updatedNamespacesFileBytes) {
		// Nothing to change
		return false, nil
	}

	if err := os.WriteFile(telemetryProxyNamespaceConfigurationsPath, updatedNamespacesFileBytes, 0644); err != nil {
		return false, fmt.Errorf("cannot write the updated namespace configuration file '%s': %w", telemetryProxyNamespaceConfigurationsPath, err)
	}

	log.Info("Updated namespace monitoring configurations", "new_configurations", newNamespaces)

	return true, nil
}
