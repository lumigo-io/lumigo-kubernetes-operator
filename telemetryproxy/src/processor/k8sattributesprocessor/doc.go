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

// Package k8sattributesprocessor allow automatic tagging of spans, metrics and logs with k8s metadata.
//
// The processor automatically discovers k8s resources (pods), extracts metadata from them and adds the
// extracted metadata to the relevant spans, metrics and logs. The processor uses the kubernetes API to discover all pods
// running in a cluster, keeps a record of their IP addresses, pod UIDs and interesting metadata.
// The rules for associating the data passing through the processor (spans, metrics and logs) with specific Pod Metadata are configured via "pod_association" key.
// It represents a list of associations that are executed in the specified order until the first one is able to do the match.
//
// Each association is specified as a list of sources of association.
// Sources represents list of rules. All rules are going to be executed and combination of result is going to be a pod metadata cache key.
// In order to get an association applied, all the data defined in each source has to be successfully fetched from a log, trace or metric.
//
// Each sources rule is specified as a pair of `from` (representing the rule type) and `name` (representing the attribute name if `From` is set to `resource_attribute`).
// Following rule types are available:
//
//	from: "connection" - takes the IP attribute from connection context (if available)
//	from: "resource_attribute" - allows to specify the attribute name to lookup up in the list of attributes of the received Resource.
//	                             Semantic convention should be used for naming.
//
// Pod association configuration.
//
//	pod_association:
//	  - sources:
//	      - from: resource_attribute
//	        name: k8s.pod.ip
//	  # below association matches for pair `k8s.pod.name` and `k8s.namespace.name`
//	  - sources:
//	      - from: resource_attribute
//	        name: k8s.pod.name
//	      - from: resource_attribute
//	        name: k8s.namespace.name
//
// If Pod association rules are not configured resources are associated with metadata only by connection's IP Address.
//
// Which metadata to collect is determined by `metadata` configuration that defines list of resource attributes
// to be added. Items in the list called exactly the same as the resource attributes that will be added.
// All the available attributes are enabled by default, you can reduce the list with `metadata` configuration.
// The following attributes will be added if pod identified:
//   - k8s.namespace.name
//   - k8s.pod.name
//   - k8s.pod.uid
//   - k8s.pod.start_time
//   - k8s.deployment.name
//   - k8s.node.name
//
// Not all the attributes are guaranteed to be added.
//
// Only attribute names from `metadata` should be used for pod_association's `resource_attribute`,
// because empty or non-existing values will be ignored.
//
// The following container level attributes require additional attributes to identify a particular container in a pod:
//  1. Container spec attributes - will be set only if container identifying attribute `k8s.container.name` is set
//     as a resource attribute (similar to all other attributes, pod has to be identified as well):
//     - container.image.name
//     - container.image.tag
//  2. Container status attributes - in addition to pod identifier and `k8s.container.name` attribute, these attributes
//     require identifier of a particular container run set as `k8s.container.restart_count` in resource attributes:
//     - container.id
//
// The k8sattributesprocessor can be used for automatic tagging of spans, metrics and logs with k8s labels and annotations from pods and namespaces.
// The config for associating the data passing through the processor (spans, metrics and logs) with specific Pod/Namespace annotations/labels is configured via "annotations"  and "labels" keys.
// This config represents a list of annotations/labels that are extracted from pods/namespaces and added to spans, metrics and logs.
// Each item is specified as a config of tag_name (representing the tag name to tag the spans with),
// key (representing the key used to extract value) and from (representing the kubernetes object used to extract the value).
// The "from" field has only two possible values "pod" and "namespace" and defaults to "pod" if none is specified.
//
// A few examples to use this config are as follows:
//
//	annotations:
//	  - tag_name: a1 # extracts value of annotation from pods with key `annotation-one` and inserts it as a tag with key `a1`
//	    key: annotation-one
//	    from: pod
//	  - tag_name: a2 # extracts value of annotation from namespaces with key `annotation-two` with regexp and inserts it as a tag with key `a2`
//	    key: annotation-two
//	    regex: field=(?P<value>.+)
//	    from: namespace
//
//	labels:
//	  - tag_name: l1 # extracts value of label from namespaces with key `label1` and inserts it as a tag with key `l1`
//	    key: label1
//	    from: namespace
//	  - tag_name: l2 # extracts value of label from pods with key `label2` with regexp and inserts it as a tag with key `l2`
//	    key: label2
//	    regex: field=(?P<value>.+)
//	    from: pod
//
// # RBAC
//
// The k8sattributesprocessor needs `get`, `watch` and `list` permissions on both `pods` and `namespaces` resources, for all namespaces and pods included in the configured filters.
// Here is an example of a `ClusterRole` to give a `ServiceAccount` the necessary permissions for all pods and namespaces in the cluster (replace `<OTEL_COL_NAMESPACE>` with a namespace where collector is deployed):
//
//	apiVersion: v1
//	kind: ServiceAccount
//	metadata:
//	  name: collector
//	  namespace: <OTEL_COL_NAMESPACE>
//	---
//	apiVersion: rbac.authorization.k8s.io/v1
//	kind: ClusterRole
//	metadata:
//	  name: otel-collector
//	rules:
//	- apiGroups: [""]
//	  resources: ["pods", "namespaces"]
//	  verbs: ["get", "watch", "list"]
//	---
//	apiVersion: rbac.authorization.k8s.io/v1
//	kind: ClusterRoleBinding
//	metadata:
//	  name: otel-collector
//	subjects:
//	- kind: ServiceAccount
//	  name: collector
//	  namespace: <OTEL_COL_NAMESPACE>
//	roleRef:
//	  kind: ClusterRole
//	  name: otel-collector
//	  apiGroup: rbac.authorization.k8s.io
//
// Config
//
//	k8sattributes:
//	k8sattributes/2:
//	  auth_type: "serviceAccount"
//	  passthrough: false
//	  filter:
//	    node_from_env_var: KUBE_NODE_NAME
//
//	  extract:
//	    metadata:
//	      - k8s.pod.name
//	      - k8s.pod.uid
//	      - k8s.deployment.name
//	      - k8s.namespace.name
//	      - k8s.node.name
//	      - k8s.pod.start_time
//
//	  pod_association:
//	   - from: resource_attribute
//	     name: k8s.pod.ip
//	   - from: resource_attribute
//	     name: k8s.pod.uid
//	   - from: connection
//
// # Deployment scenarios
//
// The processor supports running both in agent and collector mode.
//
// # As an agent
//
// When running as an agent, the processor detects IP addresses of pods sending spans, metrics or logs to the agent
// and uses this information to extract metadata from pods. When running as an agent, it is important to apply
// a discovery filter so that the processor only discovers pods from the same host that it is running on. Not using
// such a filter can result in unnecessary resource usage especially on very large clusters. Once the filter is applied,
// each processor will only query the k8s API for pods running on it's own node.
//
// Node filter can be applied by setting the `filter.node` config option to the name of a k8s node. While this works
// as expected, it cannot be used to automatically filter pods by the same node that the processor is running on in
// most cases as it is not know before hand which node a pod will be scheduled on. Luckily, kubernetes has a solution
// for this called the downward API. To automatically filter pods by the node the processor is running on, you'll need
// to complete the following steps:
//
// 1. Use the downward API to inject the node name as an environment variable.
// Add the following snippet under the pod env section of the OpenTelemetry container.
//
//	spec:
//	  containers:
//	  - env:
//	    - name: KUBE_NODE_NAME
//	      valueFrom:
//	        fieldRef:
//	          apiVersion: v1
//	          fieldPath: spec.nodeName
//
// This will inject a new environment variable to the OpenTelemetry container with the value as the
// name of the node the pod was scheduled to run on.
//
// 2. Set "filter.node_from_env_var" to the name of the environment variable holding the node name.
//
//	k8sattributes:
//	  filter:
//	    node_from_env_var: KUBE_NODE_NAME # this should be same as the var name used in previous step
//
// This will restrict each OpenTelemetry agent to query pods running on the same node only dramatically reducing
// resource requirements for very large clusters.
//
// # As a collector
//
// The processor can be deployed both as an agent or as a collector.
//
// When running as a collector, the processor cannot correctly detect the IP address of the pods generating
// the telemetry data without any of the well-known IP attributes, when it receives them
// from an agent instead of receiving them directly from the pods. To
// workaround this issue, agents deployed with the k8sattributes processor can be configured to detect
// the IP addresses and forward them along with the telemetry data resources. Collector can then match this IP address
// with k8s pods and enrich the records with the metadata. In order to set this up, you'll need to complete the
// following steps:
//
// 1. Setup agents in passthrough mode
// Configure the agents' k8sattributes processors to run in passthrough mode.
//
//	# k8sattributes config for agent
//	k8sattributes:
//	  passthrough: true
//
// This will ensure that the agents detect the IP address as add it as an attribute to all telemetry resources.
// Agents will not make any k8s API calls, do any discovery of pods or extract any metadata.
//
// 2. Configure the collector as usual
// No special configuration changes are needed to be made on the collector. It'll automatically detect
// the IP address of spans, logs and metrics sent by the agents as well as directly by other services/pods.
//
// # Caveats
//
// There are some edge-cases and scenarios where k8sattributes will not work properly.
//
// # Host networking mode
//
// The processor cannot correct identify pods running in the host network mode and
// enriching telemetry data generated by such pods is not supported at the moment, unless the association
// rule is not based on IP attribute.
//
// # As a sidecar
//
// The processor does not support detecting containers from the same pods when running
// as a sidecar. While this can be done, we think it is simpler to just use the kubernetes
// downward API to inject environment variables into the pods and directly use their values
// as tags.
package k8sattributesprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sattributesprocessor"
