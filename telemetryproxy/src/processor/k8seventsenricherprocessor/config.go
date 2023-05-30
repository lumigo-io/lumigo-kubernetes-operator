// Copyright 2020 OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8seventsenricherprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8seventsenricherprocessor"

import (
	"k8s.io/client-go/dynamic"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/k8sconfig"
)

type Config struct {
	k8sconfig.APIConfig `mapstructure:",squash"`

	// For mocking purposes only.
	makeDynamicClient func() (dynamic.Interface, error)
}

func (c *Config) getDynamicClient() (dynamic.Interface, error) {
	if c.makeDynamicClient != nil {
		return c.makeDynamicClient()
	}

	return k8sconfig.MakeDynamicClient(c.APIConfig)
}
