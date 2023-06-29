/*
Copyright 2023 Lumigo.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package telemetryproxyconfigs

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	letterRunes                     = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	telemetryProxyNamespacesTempDir string
	logger                          logr.Logger
)

func TestAPIs(t *testing.T) {
	logger = testr.New(t)

	telemetryProxyNamespacesTempDir = t.TempDir()

	RegisterFailHandler(Fail)

	RunSpecs(t, "Namespace Monitoring Configs Suite")
}

func createEmptyNamespaceFile() string {
	b := make([]rune, 10)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}

	telemetryProxyNamespacesFile := telemetryProxyNamespacesTempDir + fmt.Sprintf("/namespaces-%s.json", string(b))

	emptyNamespacesFileBytes, err := json.Marshal(make([]NamespaceMonitoringConfig, 0))
	if err != nil {
		panic(fmt.Errorf("cannot marshal the updated namespace configuration: %w", err))
	}

	if err := os.WriteFile(telemetryProxyNamespacesFile, emptyNamespacesFileBytes, 0644); err != nil {
		panic(fmt.Errorf("cannot write the updated namespace configuration file '%s': %w", telemetryProxyNamespacesFile, err))
	}

	return telemetryProxyNamespacesFile
}

func parseJsonFile(filePath string) []NamespaceMonitoringConfig {
	actualBytes, err := os.ReadFile(filePath)
	if err != nil {
		panic(err)
	}

	var monitoredNamespaces []NamespaceMonitoringConfig
	if err := json.Unmarshal(actualBytes, &monitoredNamespaces); err != nil {
		panic(err)
	}

	return monitoredNamespaces
}

var _ = Context("Lumigo controller", func() {

	It("Adds a namespace correctly", func() {
		file := createEmptyNamespaceFile()

		testConfig := &NamespaceMonitoringConfig{
			Name:  "ns-test",
			Uid:   "123456",
			Token: "t_123456",
		}

		UpsertTelemetryProxyMonitoringOfNamespace(context.TODO(), file, testConfig.Name, testConfig.Uid, testConfig.Token, &logger)

		Expect(parseJsonFile(file)).To(ContainElement(*testConfig))
	})

	It("Upserts and removes a namespace correctly", func() {
		file := createEmptyNamespaceFile()

		testConfig := &NamespaceMonitoringConfig{
			Name:  "ns-test",
			Uid:   "123456",
			Token: "t_123456",
		}

		UpsertTelemetryProxyMonitoringOfNamespace(context.TODO(), file, testConfig.Name, testConfig.Uid, testConfig.Token, &logger)

		Expect(parseJsonFile(file)).To(ContainElement(*testConfig))

		RemoveTelemetryProxyMonitoringOfNamespace(context.TODO(), file, testConfig.Name, &logger)

		Expect(parseJsonFile(file)).NotTo(ContainElement(*testConfig))
	})

	It("Upserts multiple times and removes a namespace correctly", func() {
		file := createEmptyNamespaceFile()

		testConfig := &NamespaceMonitoringConfig{
			Name:  "ns-test",
			Uid:   "123456",
			Token: "t_123456",
		}

		modified, err := UpsertTelemetryProxyMonitoringOfNamespace(context.TODO(), file, testConfig.Name, testConfig.Uid, testConfig.Token, &logger)
		Expect(modified).To(BeTrue())
		Expect(err).NotTo(HaveOccurred())

		// Adding the same namespace multiple times is idempotent
		modified, err = UpsertTelemetryProxyMonitoringOfNamespace(context.TODO(), file, testConfig.Name, testConfig.Uid, testConfig.Token, &logger)
		Expect(modified).To(BeFalse())
		Expect(err).NotTo(HaveOccurred())

		Expect(parseJsonFile(file)).To(HaveLen(1))
		Expect(parseJsonFile(file)).To(ContainElement(*testConfig))

		modified, err = RemoveTelemetryProxyMonitoringOfNamespace(context.TODO(), file, testConfig.Name, &logger)
		Expect(modified).To(BeTrue())
		Expect(err).NotTo(HaveOccurred())

		Expect(parseJsonFile(file)).NotTo(ContainElement(*testConfig))

		// Removing the same namespace multiple times is idempotent
		modified, err = RemoveTelemetryProxyMonitoringOfNamespace(context.TODO(), file, testConfig.Name, &logger)
		Expect(modified).To(BeFalse())
		Expect(err).NotTo(HaveOccurred())

		Expect(parseJsonFile(file)).NotTo(ContainElement(*testConfig))
	})

})
