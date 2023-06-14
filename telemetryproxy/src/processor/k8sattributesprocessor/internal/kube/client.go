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

package kube // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sattributesprocessor/internal/kube"

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
	apps_v1 "k8s.io/api/apps/v1"
	batch_v1 "k8s.io/api/batch/v1"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/k8sconfig"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sattributesprocessor/internal/observability"
)

// WatchClient is the main interface provided by this package to a kubernetes cluster.
type WatchClient struct {
	m                     sync.RWMutex
	deleteMut             sync.Mutex
	logger                *zap.Logger
	kc                    kubernetes.Interface
	cronJobInformer       cache.SharedInformer
	deploymentInformer    cache.SharedInformer
	podInformer           cache.SharedInformer
	namespaceInformer     cache.SharedInformer
	replicasetRegex       *regexp.Regexp
	cronJobRegex          *regexp.Regexp
	deletePodQueue        []deletePodRequest
	deleteDeploymentQueue []deleteDeploymentRequest
	deleteCronJobQueue    []deleteCronJobRequest
	stopCh                chan struct{}

	// A map containing Pod related data, used to associate them with resources.
	// Key can be either an IP address or Pod UID
	Pods         map[PodIdentifier]*Pod
	Rules        ExtractionRules
	Filters      Filters
	Associations []Association
	Exclude      Excludes

	// A map containing Namespace related data, used to associate them with resources.
	// Key is namespace name
	Namespaces map[string]*Namespace

	// A map containing Deployment related data, used to associate them with resources.
	// Key is "<namespace_name>/<name>""
	Deployments map[string]*Deployment

	// A map containing CronJob related data, used to associate them with resources.
	// Key is "<namespace_name>/<name>""
	CronJobs map[string]*CronJob
}

// Extract replicaset name from the pod name. Pod name is created using
// format: [deployment-name]-[Random-String-For-ReplicaSet]
var rRegex = regexp.MustCompile(`^(.*)-[0-9a-zA-Z]+$`)

// Extract CronJob name from the Job name. Job name is created using
// format: [cronjob-name]-[time-hash-int]
var cronJobRegex = regexp.MustCompile(`^(.*)-[0-9]+$`)

// New initializes a new k8s Client.
func New(logger *zap.Logger, apiCfg k8sconfig.APIConfig, rules ExtractionRules, filters Filters, associations []Association, exclude Excludes, newClientSet APIClientsetProvider, newPodInformer InformerProvider, newDeploymentInformer InformerProvider, newCronJobInformer InformerProvider, newNamespaceInformer InformerProviderNamespace) (Client, error) {
	c := &WatchClient{
		logger:          logger,
		Rules:           rules,
		Filters:         filters,
		Associations:    associations,
		Exclude:         exclude,
		replicasetRegex: rRegex,
		cronJobRegex:    cronJobRegex,
		stopCh:          make(chan struct{}),
	}
	go c.deleteLoop(time.Second*30, defaultPodDeleteGracePeriod)

	c.logger.Debug("New WatchClient", zap.Any("extraction-rules", rules))

	c.Pods = map[PodIdentifier]*Pod{}
	c.CronJobs = map[string]*CronJob{}
	c.Deployments = map[string]*Deployment{}
	c.Namespaces = map[string]*Namespace{}
	if newClientSet == nil {
		newClientSet = k8sconfig.MakeClient
	}

	kc, err := newClientSet(apiCfg)
	if err != nil {
		return nil, err
	}
	c.kc = kc

	labelSelector, fieldSelector, err := selectorsFromFilters(c.Filters)
	if err != nil {
		return nil, err
	}
	logger.Info(
		"k8s filtering",
		zap.String("labelSelector", labelSelector.String()),
		zap.String("fieldSelector", fieldSelector.String()),
	)
	if newPodInformer == nil {
		newPodInformer = newSharedPodInformer
	}

	if newDeploymentInformer == nil {
		newDeploymentInformer = newSharedDeploymentInformer
	}

	if newCronJobInformer == nil {
		newCronJobInformer = newSharedCronJobInformer
	}

	if newNamespaceInformer == nil {
		newNamespaceInformer = newNamespaceSharedInformer
	}

	c.podInformer = newPodInformer(c.kc, c.Filters.Namespace, labelSelector, fieldSelector)

	if needCronJobAttributes(c.Rules) {
		// TODO Get only necessary fields
		c.cronJobInformer = newCronJobInformer(c.kc, c.Filters.Namespace, labels.Everything(), fields.Everything())
	} else {
		c.cronJobInformer = NewNoOpInformer(c.kc)
	}

	if needDeploymentAttributes(c.Rules) {
		// TODO Get only necessary fields
		c.deploymentInformer = newDeploymentInformer(c.kc, c.Filters.Namespace, labels.Everything(), fields.Everything())
	} else {
		c.deploymentInformer = NewNoOpInformer(c.kc)
	}

	if needNamespaceAttributes(c.Rules) {
		c.namespaceInformer = newNamespaceInformer(c.kc)
	} else {
		c.namespaceInformer = NewNoOpInformer(c.kc)
	}

	return c, err
}

// Start registers pod event handlers and starts watching the kubernetes cluster for pod changes.
func (c *WatchClient) Start() {
	if _, err := c.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.handlePodAdd,
		UpdateFunc: c.handlePodUpdate,
		DeleteFunc: c.handlePodDelete,
	}); err != nil {
		c.logger.Error("error adding event handler to pod informer", zap.Error(err))
	}
	c.logger.Debug("Starting pod informer")
	go c.podInformer.Run(c.stopCh)

	if _, err := c.cronJobInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.handleCronJobAdd,
		UpdateFunc: c.handleCronJobUpdate,
		DeleteFunc: c.handleCronJobDelete,
	}); err != nil {
		c.logger.Error("error adding event handler to cronjob informer", zap.Error(err))
	}
	c.logger.Debug("Starting cronjob informer")
	go c.cronJobInformer.Run(c.stopCh)

	if _, err := c.deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.handleDeploymentAdd,
		UpdateFunc: c.handleDeploymentUpdate,
		DeleteFunc: c.handleDeploymentDelete,
	}); err != nil {
		c.logger.Error("error adding event handler to deployment informer", zap.Error(err))
	}
	c.logger.Debug("Starting deployment informer")
	go c.deploymentInformer.Run(c.stopCh)

	if _, err := c.namespaceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.handleNamespaceAdd,
		UpdateFunc: c.handleNamespaceUpdate,
		DeleteFunc: c.handleNamespaceDelete,
	}); err != nil {
		c.logger.Error("error adding event handler to namespace informer", zap.Error(err))
	}
	c.logger.Debug("Starting namespace informer")
	go c.namespaceInformer.Run(c.stopCh)
}

// Stop signals the the k8s watcher/informer to stop watching for new events.
func (c *WatchClient) Stop() {
	close(c.stopCh)
}

func (c *WatchClient) handlePodAdd(obj interface{}) {
	if pod, ok := obj.(*api_v1.Pod); ok {
		c.addOrUpdatePod(pod)

		observability.RecordPodAdded()
		observability.RecordPodTableSize(int64(len(c.Pods)))
	} else {
		c.logger.Error("object received was not of type v1.Pod", zap.Any("received", obj))
	}
}

func (c *WatchClient) handlePodUpdate(old, new interface{}) {
	if pod, ok := new.(*api_v1.Pod); ok {
		// TODO: update or remove based on whether container is ready/unready?.
		c.addOrUpdatePod(pod)

		observability.RecordPodUpdated()
		observability.RecordPodTableSize(int64(len(c.Pods)))
	} else {
		c.logger.Error("object received was not of type v1.Pod", zap.Any("received", new))
	}
}

func (c *WatchClient) handlePodDelete(obj interface{}) {
	if pod, ok := obj.(*api_v1.Pod); ok {
		c.forgetPod(pod)

		observability.RecordPodDeleted()
		observability.RecordPodTableSize(int64(len(c.Pods)))
	} else {
		c.logger.Error("object received was not of type v1.Pod", zap.Any("received", obj))
	}
}

func (c *WatchClient) handleDeploymentAdd(obj interface{}) {
	if deployment, ok := obj.(*apps_v1.Deployment); ok {
		c.addOrUpdateDeployment(deployment)

		observability.RecordDeploymentAdded()
		observability.RecordDeploymentTableSize(int64(len(c.Deployments)))
	} else {
		c.logger.Error("object received was not of type apps/v1.Deployment", zap.Any("received", obj))
	}
}

func (c *WatchClient) handleDeploymentUpdate(old, new interface{}) {
	if deployment, ok := new.(*apps_v1.Deployment); ok {
		c.addOrUpdateDeployment(deployment)

		observability.RecordDeploymentUpdated()
		observability.RecordDeploymentTableSize(int64(len(c.Deployments)))
	} else {
		c.logger.Error("object received was not of type apps/v1.Deployment", zap.Any("received", new))
	}
}

func (c *WatchClient) handleDeploymentDelete(obj interface{}) {
	if deployment, ok := obj.(*apps_v1.Deployment); ok {
		c.forgetDeployment(deployment)

		observability.RecordDeploymentDeleted()
		observability.RecordDeploymentTableSize(int64(len(c.Deployments)))
	} else {
		c.logger.Error("object received was not of type apps/v1.Deployment", zap.Any("received", obj))
	}
}

func (c *WatchClient) handleCronJobAdd(obj interface{}) {
	if cronJob, ok := obj.(*batch_v1.CronJob); ok {
		c.addOrUpdateCronJob(cronJob)

		observability.RecordCronJobAdded()
		observability.RecordCronJobTableSize(int64(len(c.CronJobs)))
	} else {
		c.logger.Error("object received was not of type batch/v1.CronJob", zap.Any("received", obj))
	}
}

func (c *WatchClient) handleCronJobUpdate(old, new interface{}) {
	if cronJob, ok := new.(*batch_v1.CronJob); ok {
		c.addOrUpdateCronJob(cronJob)

		observability.RecordCronJobUpdated()
		observability.RecordCronJobTableSize(int64(len(c.CronJobs)))
	} else {
		c.logger.Error("object received was not of type batch/v1.CronJob", zap.Any("received", new))
	}
}

func (c *WatchClient) handleCronJobDelete(obj interface{}) {
	if cronJob, ok := obj.(*batch_v1.CronJob); ok {
		c.forgetCronJob(cronJob)

		observability.RecordCronJobDeleted()
		observability.RecordCronJobTableSize(int64(len(c.CronJobs)))
	} else {
		c.logger.Error("object received was not of type batch/v1.CronJob", zap.Any("received", obj))
	}
}

func (c *WatchClient) handleNamespaceAdd(obj interface{}) {
	if namespace, ok := obj.(*api_v1.Namespace); ok {
		c.addOrUpdateNamespace(namespace)

		observability.RecordNamespaceAdded()
	} else {
		c.logger.Error("object received was not of type v1.Namespace", zap.Any("received", obj))
	}
}

func (c *WatchClient) handleNamespaceUpdate(old, new interface{}) {
	if namespace, ok := new.(*api_v1.Namespace); ok {
		c.addOrUpdateNamespace(namespace)

		observability.RecordNamespaceUpdated()
	} else {
		c.logger.Error("object received was not of type v1.Namespace", zap.Any("received", new))
	}
}

func (c *WatchClient) handleNamespaceDelete(obj interface{}) {
	if namespace, ok := obj.(*api_v1.Namespace); ok {
		c.m.Lock()
		if ns, ok := c.Namespaces[namespace.Name]; ok {
			// When a namespace is deleted all the pods(and other k8s objects in that namespace) in that namespace are deleted before it.
			// So we wont have any spans that might need namespace annotations and labels.
			// Thats why we dont need an implementation for deleteQueue and gracePeriod for namespaces.
			delete(c.Namespaces, ns.Name)
		}
		c.m.Unlock()

		observability.RecordNamespaceDeleted()
	} else {
		c.logger.Error("object received was not of type v1.Namespace", zap.Any("received", obj))
	}
}

func (c *WatchClient) deleteLoop(interval time.Duration, gracePeriod time.Duration) {
	// This loop runs after N seconds and deletes pods from cache.
	// It iterates over the delete queue and deletes all that aren't
	// in the grace period anymore.
	for {
		select {
		case <-time.After(interval):
			var cutoff int
			now := time.Now()
			c.deleteMut.Lock()
			for i, d := range c.deletePodQueue {
				if d.ts.Add(gracePeriod).After(now) {
					break
				}
				cutoff = i + 1
			}
			podsToDelete := c.deletePodQueue[:cutoff]
			c.deletePodQueue = c.deletePodQueue[cutoff:]

			cutoff = 0
			for i, d := range c.deleteDeploymentQueue {
				if d.ts.Add(gracePeriod).After(now) {
					break
				}
				cutoff = i + 1
			}
			deploymentsToDelete := c.deleteDeploymentQueue[:cutoff]
			c.deleteDeploymentQueue = c.deleteDeploymentQueue[cutoff:]

			cutoff = 0
			for i, d := range c.deleteCronJobQueue {
				if d.ts.Add(gracePeriod).After(now) {
					break
				}
				cutoff = i + 1
			}
			cronJobsToDelete := c.deleteCronJobQueue[:cutoff]
			c.deleteCronJobQueue = c.deleteCronJobQueue[cutoff:]

			c.deleteMut.Unlock()

			c.m.Lock()
			for _, d := range cronJobsToDelete {
				if p, ok := c.CronJobs[d.id]; ok {
					// Sanity check: make sure we are deleting the same pod
					// and the underlying state (ip<>pod mapping) has not changed.
					if p.Name == d.cronJobName {
						delete(c.CronJobs, d.id)
					}
				}
			}
			observability.RecordCronJobTableSize(int64(len(c.CronJobs)))

			for _, d := range deploymentsToDelete {
				if p, ok := c.Deployments[d.id]; ok {
					// Sanity check: make sure we are deleting the same pod
					// and the underlying state (ip<>pod mapping) has not changed.
					if p.Name == d.deploymentName {
						delete(c.Deployments, d.id)
					}
				}
			}
			observability.RecordDeploymentTableSize(int64(len(c.Deployments)))

			for _, d := range podsToDelete {
				if p, ok := c.Pods[d.id]; ok {
					// Sanity check: make sure we are deleting the same pod
					// and the underlying state (ip<>pod mapping) has not changed.
					if p.Name == d.podName {
						delete(c.Pods, d.id)
					}
				}
			}
			observability.RecordPodTableSize(int64(len(c.Pods)))
			c.m.Unlock()

		case <-c.stopCh:
			return
		}
	}
}

// GetPod takes an IP address or Pod UID and returns the pod the identifier is associated with.
func (c *WatchClient) GetPod(identifier PodIdentifier) (*Pod, bool) {
	c.m.RLock()
	pod, ok := c.Pods[identifier]
	c.m.RUnlock()
	if ok {
		if pod.Ignore {
			return nil, false
		}
		return pod, ok
	}
	observability.RecordIPLookupMiss()
	return nil, false
}

// GetNamespace takes a namespace and returns the namespace object the namespace is associated with.
func (c *WatchClient) GetNamespace(namespace string) (*Namespace, bool) {
	c.m.RLock()
	ns, ok := c.Namespaces[namespace]
	c.m.RUnlock()
	if ok {
		return ns, ok
	}
	return nil, false
}

// GetNamespace takes a namespace and returns the namespace object the namespace is associated with.
func (c *WatchClient) GetDeployment(namespace, name string) (*Deployment, bool) {
	cacheKey := fmt.Sprintf("%s/%s", namespace, name)

	c.m.RLock()
	d, ok := c.Deployments[cacheKey]
	c.m.RUnlock()
	if ok {
		return d, ok
	}
	return nil, false
}

// GetNamespace takes a namespace and returns the namespace object the namespace is associated with.
func (c *WatchClient) GetCronJob(namespace, name string) (*CronJob, bool) {
	cacheKey := fmt.Sprintf("%s/%s", namespace, name)

	c.m.RLock()
	cj, ok := c.CronJobs[cacheKey]
	c.m.RUnlock()
	if ok {
		return cj, ok
	}
	return nil, false
}

func (c *WatchClient) extractPodAttributes(pod *api_v1.Pod) map[string]string {
	tags := map[string]string{}
	if c.Rules.PodName {
		tags[conventions.AttributeK8SPodName] = pod.Name
	}

	if c.Rules.NamespaceName {
		tags[conventions.AttributeK8SNamespaceName] = pod.GetNamespace()
	}

	if c.Rules.NamespaceUID {
		namespaceName := pod.GetNamespace()
		if namespace, isOk := c.GetNamespace(namespaceName); isOk {
			tags["k8s.namespace.uid"] = namespace.NamespaceUID
		} else {
			c.logger.Debug(
				fmt.Sprintf("Namespace not found in informer cache, '%s' attribute will not be filled", "k8s.namespace.uid"),
				zap.String("namespace", namespaceName),
			)
		}
	}

	if c.Rules.StartTime {
		ts := pod.GetCreationTimestamp()
		if !ts.IsZero() {
			tags[tagStartTime] = ts.String()
		}
	}

	if c.Rules.PodUID {
		uid := pod.GetUID()
		tags[conventions.AttributeK8SPodUID] = string(uid)
	}

	if c.Rules.ReplicaSetUID || c.Rules.ReplicaSetName ||
		c.Rules.DaemonSetUID || c.Rules.DaemonSetName ||
		c.Rules.JobUID || c.Rules.JobName ||
		c.Rules.StatefulSetUID || c.Rules.StatefulSetName ||
		c.Rules.DeploymentName || c.Rules.DeploymentUID ||
		c.Rules.CronJobName || c.Rules.CronJobUID {
		for _, ref := range pod.OwnerReferences {
			switch ref.Kind {
			case "ReplicaSet":
				if c.Rules.ReplicaSetUID {
					tags[conventions.AttributeK8SReplicaSetUID] = string(ref.UID)
				}
				if c.Rules.ReplicaSetName {
					tags[conventions.AttributeK8SReplicaSetName] = ref.Name
				}

				if c.Rules.DeploymentName || c.Rules.DeploymentUID {
					parts := c.replicasetRegex.FindStringSubmatch(ref.Name)
					if len(parts) == 2 {
						deploymentName := parts[1]

						if c.Rules.DeploymentName {
							tags[conventions.AttributeK8SDeploymentName] = deploymentName
						}

						if c.Rules.DeploymentUID {
							namespace := pod.GetNamespace()
							if d, isOk := c.GetDeployment(namespace, deploymentName); isOk {
								tags[conventions.AttributeK8SDeploymentUID] = d.UID
							} else {
								c.logger.Debug(
									fmt.Sprintf("Deployment not found in informer cache, '%s' attribute will not be filled", conventions.AttributeK8SDeploymentUID),
									zap.String("namespace", namespace),
									zap.String("deployment", deploymentName),
								)
							}
						}
					}
				}
			case "DaemonSet":
				if c.Rules.DaemonSetUID {
					tags[conventions.AttributeK8SDaemonSetUID] = string(ref.UID)
				}
				if c.Rules.DaemonSetName {
					tags[conventions.AttributeK8SDaemonSetName] = ref.Name
				}
			case "StatefulSet":
				if c.Rules.StatefulSetUID {
					tags[conventions.AttributeK8SStatefulSetUID] = string(ref.UID)
				}
				if c.Rules.StatefulSetName {
					tags[conventions.AttributeK8SStatefulSetName] = ref.Name
				}
			case "Job":
				if c.Rules.JobUID {
					tags[conventions.AttributeK8SJobUID] = string(ref.UID)
				}
				if c.Rules.JobName {
					tags[conventions.AttributeK8SJobName] = ref.Name
				}

				if c.Rules.CronJobName || c.Rules.CronJobUID {
					parts := c.cronJobRegex.FindStringSubmatch(ref.Name)
					if len(parts) == 2 {
						cronJobName := parts[1]

						if c.Rules.CronJobName {
							tags[conventions.AttributeK8SCronJobName] = cronJobName
						}

						if c.Rules.CronJobUID {
							namespace := pod.GetNamespace()
							if cj, isOk := c.GetCronJob(namespace, cronJobName); isOk {
								tags[conventions.AttributeK8SCronJobUID] = cj.UID
							} else {
								c.logger.Debug(
									fmt.Sprintf("CronJob not found in informer cache, '%s' attribute will not be filled", conventions.AttributeK8SCronJobUID),
									zap.String("namespace", namespace),
									zap.String("cronjob.name", cronJobName),
								)
							}
						}
					}
				}
			}
		}
	}

	if c.Rules.Node {
		tags[tagNodeName] = pod.Spec.NodeName
	}

	for _, r := range c.Rules.Labels {
		r.extractFromPodMetadata(pod.Labels, tags, "k8s.pod.labels.%s")
	}

	for _, r := range c.Rules.Annotations {
		r.extractFromPodMetadata(pod.Annotations, tags, "k8s.pod.annotations.%s")
	}
	return tags
}

func (c *WatchClient) extractPodContainersAttributes(pod *api_v1.Pod) map[string]*Container {
	containers := map[string]*Container{}

	if c.Rules.ContainerImageName || c.Rules.ContainerImageTag {
		for _, spec := range append(pod.Spec.Containers, pod.Spec.InitContainers...) {
			container := &Container{}
			imageParts := strings.Split(spec.Image, ":")
			if c.Rules.ContainerImageName {
				container.ImageName = imageParts[0]
			}
			if c.Rules.ContainerImageTag && len(imageParts) > 1 {
				container.ImageTag = imageParts[1]
			}
			containers[spec.Name] = container
		}
	}

	if c.Rules.ContainerID {
		for _, apiStatus := range append(pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses...) {
			container, ok := containers[apiStatus.Name]
			if !ok {
				container = &Container{}
				containers[apiStatus.Name] = container
			}
			if container.Statuses == nil {
				container.Statuses = map[int]ContainerStatus{}
			}

			containerID := apiStatus.ContainerID

			// Remove container runtime prefix
			idParts := strings.Split(containerID, "://")
			if len(idParts) == 2 {
				containerID = idParts[1]
			}

			container.Statuses[int(apiStatus.RestartCount)] = ContainerStatus{containerID}
		}
	}
	return containers
}

func (c *WatchClient) extractNamespaceAttributes(namespace *api_v1.Namespace) map[string]string {
	tags := map[string]string{}

	for _, r := range c.Rules.Labels {
		r.extractFromNamespaceMetadata(namespace.Labels, tags, "k8s.namespace.labels.%s")
	}

	for _, r := range c.Rules.Annotations {
		r.extractFromNamespaceMetadata(namespace.Annotations, tags, "k8s.namespace.annotations.%s")
	}

	return tags
}

func (c *WatchClient) podFromAPI(pod *api_v1.Pod) *Pod {
	newPod := &Pod{
		Name:        pod.Name,
		Namespace:   pod.GetNamespace(),
		Address:     pod.Status.PodIP,
		HostNetwork: pod.Spec.HostNetwork,
		PodUID:      string(pod.UID),
		StartTime:   pod.Status.StartTime,
	}

	if c.shouldIgnorePod(pod) {
		newPod.Ignore = true
	} else {
		newPod.Attributes = c.extractPodAttributes(pod)
		if needContainerAttributes(c.Rules) {
			newPod.Containers = c.extractPodContainersAttributes(pod)
		}
	}

	return newPod
}

func (c *WatchClient) deploymentFromAPI(deployment *apps_v1.Deployment) *Deployment {
	namespace, isOk := c.GetNamespace(deployment.Namespace)
	if !isOk {
		namespace = &Namespace{}
	}

	return &Deployment{
		Name:          deployment.Name,
		UID:           string(deployment.UID),
		NamespaceName: namespace.Name,
		NamespaceUID:  namespace.NamespaceUID,
	}
}

func (c *WatchClient) cronJobFromAPI(cronJob *batch_v1.CronJob) *CronJob {
	namespace, isOk := c.GetNamespace(cronJob.Namespace)
	if !isOk {
		namespace = &Namespace{}
	}

	return &CronJob{
		Name:          cronJob.Name,
		UID:           string(cronJob.UID),
		NamespaceName: namespace.Name,
		NamespaceUID:  namespace.NamespaceUID,
	}
}

// getIdentifiersFromAssoc returns list of PodIdentifiers for given pod
func (c *WatchClient) getIdentifiersFromAssoc(pod *Pod) []PodIdentifier {
	var ids []PodIdentifier
	for _, assoc := range c.Associations {
		ret := PodIdentifier{}
		skip := false
		for i, source := range assoc.Sources {
			// If association configured to take IP address from connection
			switch {
			case source.From == ConnectionSource:
				if pod.Address == "" {
					skip = true
					break
				}
				// Host network mode is not supported right now with IP based
				// tagging as all pods in host network get same IP addresses.
				// Such pods are very rare and usually are used to monitor or control
				// host traffic (e.g, linkerd, flannel) instead of service business needs.
				if pod.HostNetwork {
					skip = true
					break
				}
				ret[i] = PodIdentifierAttributeFromSource(source, pod.Address)
			case source.From == ResourceSource:
				attr := ""
				switch source.Name {
				case conventions.AttributeK8SNamespaceName:
					attr = pod.Namespace
				case conventions.AttributeK8SPodName:
					attr = pod.Name
				case conventions.AttributeK8SPodUID:
					attr = pod.PodUID
				case conventions.AttributeHostName:
					attr = pod.Address
				// k8s.pod.ip is set by passthrough mode
				case K8sIPLabelName:
					attr = pod.Address
				default:
					if v, ok := pod.Attributes[source.Name]; ok {
						attr = v
					}
				}

				if attr == "" {
					skip = true
					break
				}
				ret[i] = PodIdentifierAttributeFromSource(source, attr)
			}
		}

		if !skip {
			ids = append(ids, ret)
		}
	}

	// Ensure backward compatibility
	if pod.PodUID != "" {
		ids = append(ids, PodIdentifier{
			PodIdentifierAttributeFromResourceAttribute(conventions.AttributeK8SPodUID, pod.PodUID),
		})
	}

	if pod.Address != "" && !pod.HostNetwork {
		ids = append(ids, PodIdentifier{
			PodIdentifierAttributeFromConnection(pod.Address),
		})
		// k8s.pod.ip is set by passthrough mode
		ids = append(ids, PodIdentifier{
			PodIdentifierAttributeFromResourceAttribute(K8sIPLabelName, pod.Address),
		})
	}

	return ids
}

func (c *WatchClient) addOrUpdatePod(pod *api_v1.Pod) {
	newPod := c.podFromAPI(pod)

	c.m.Lock()
	defer c.m.Unlock()

	for _, id := range c.getIdentifiersFromAssoc(newPod) {
		// compare initial scheduled timestamp for existing pod and new pod with same identifier
		// and only replace old pod if scheduled time of new pod is newer or equal.
		// This should fix the case where scheduler has assigned the same attributes (like IP address)
		// to a new pod but update event for the old pod came in later.
		if p, ok := c.Pods[id]; ok {
			if pod.Status.StartTime.Before(p.StartTime) {
				continue
			}
		}
		c.Pods[id] = newPod
	}
}

func (c *WatchClient) addOrUpdateDeployment(deployment *apps_v1.Deployment) {
	newDeployment := c.deploymentFromAPI(deployment)
	cacheKey := newDeployment.cacheKey()

	c.m.Lock()
	c.Deployments[cacheKey] = newDeployment
	c.m.Unlock()

	c.logger.Debug("Upserted deployment", zap.String("cache-key", cacheKey), zap.Any("deployment", newDeployment))
}

func (c *WatchClient) addOrUpdateCronJob(cronJob *batch_v1.CronJob) {
	newCronJob := c.cronJobFromAPI(cronJob)
	cacheKey := newCronJob.cacheKey()

	c.m.Lock()
	c.CronJobs[cacheKey] = newCronJob
	c.m.Unlock()

	c.logger.Debug("Upserted cronjob", zap.String("cache-key", cacheKey), zap.Any("cronjob", newCronJob))
}

func (c *WatchClient) forgetPod(pod *api_v1.Pod) {
	podToRemove := c.podFromAPI(pod)
	for _, id := range c.getIdentifiersFromAssoc(podToRemove) {
		p, ok := c.GetPod(id)

		if ok && p.Name == pod.Name {
			c.appendDeletePodQueue(id, pod.Name)
		}
	}
}

func (c *WatchClient) forgetDeployment(deployment *apps_v1.Deployment) {
	deploymentToRemove := c.deploymentFromAPI(deployment)
	c.appendDeleteDeploymentQueue(deploymentToRemove)
}

func (c *WatchClient) forgetCronJob(cronJob *batch_v1.CronJob) {
	cronJobToRemove := c.cronJobFromAPI(cronJob)
	c.appendDeleteCronJobQueue(cronJobToRemove)
}

func (c *WatchClient) appendDeletePodQueue(podID PodIdentifier, podName string) {
	c.deleteMut.Lock()
	c.deletePodQueue = append(c.deletePodQueue, deletePodRequest{
		id:      podID,
		podName: podName,
		ts:      time.Now(),
	})
	c.deleteMut.Unlock()
}

func (c *WatchClient) appendDeleteDeploymentQueue(deployment *Deployment) {
	c.deleteMut.Lock()
	c.deleteDeploymentQueue = append(c.deleteDeploymentQueue, deleteDeploymentRequest{
		id:             deployment.cacheKey(),
		deploymentName: deployment.Name,
		deploymentUID:  deployment.UID,
		namespaceName:  deployment.NamespaceName,
		ts:             time.Now(),
	})
	c.deleteMut.Unlock()
}

func (c *WatchClient) appendDeleteCronJobQueue(cronJob *CronJob) {
	c.deleteMut.Lock()
	c.deleteCronJobQueue = append(c.deleteCronJobQueue, deleteCronJobRequest{
		id:            cronJob.cacheKey(),
		cronJobName:   cronJob.Name,
		cronJobUID:    cronJob.UID,
		namespaceName: cronJob.NamespaceName,
		ts:            time.Now(),
	})
	c.deleteMut.Unlock()
}

func (c *WatchClient) shouldIgnorePod(pod *api_v1.Pod) bool {
	// Check if user requested the pod to be ignored through annotations
	if v, ok := pod.Annotations[ignoreAnnotation]; ok {
		if strings.ToLower(strings.TrimSpace(v)) == "true" {
			return true
		}
	}

	// Check if user requested the pod to be ignored through configuration
	for _, excludedPod := range c.Exclude.Pods {
		if excludedPod.Name.MatchString(pod.Name) {
			return true
		}
	}

	return false
}

func selectorsFromFilters(filters Filters) (labels.Selector, fields.Selector, error) {
	labelSelector := labels.Everything()
	for _, f := range filters.Labels {
		r, err := labels.NewRequirement(f.Key, f.Op, []string{f.Value})
		if err != nil {
			return nil, nil, err
		}
		labelSelector = labelSelector.Add(*r)
	}

	var selectors []fields.Selector
	for _, f := range filters.Fields {
		switch f.Op {
		case selection.Equals:
			selectors = append(selectors, fields.OneTermEqualSelector(f.Key, f.Value))
		case selection.NotEquals:
			selectors = append(selectors, fields.OneTermNotEqualSelector(f.Key, f.Value))
		default:
			return nil, nil, fmt.Errorf("field filters don't support operator: '%s'", f.Op)
		}
	}

	if filters.Node != "" {
		selectors = append(selectors, fields.OneTermEqualSelector(podNodeField, filters.Node))
	}
	return labelSelector, fields.AndSelectors(selectors...), nil
}

func (c *WatchClient) addOrUpdateNamespace(namespace *api_v1.Namespace) {
	newNamespace := &Namespace{
		Name:         namespace.Name,
		NamespaceUID: string(namespace.UID),
		StartTime:    namespace.GetCreationTimestamp(),
	}
	newNamespace.Attributes = c.extractNamespaceAttributes(namespace)

	c.m.Lock()
	if namespace.Name != "" {
		c.Namespaces[namespace.Name] = newNamespace
	}
	c.m.Unlock()

	c.logger.Debug("Upserted namespace", zap.Any("namespace", newNamespace))
}

func needCronJobAttributes(rules ExtractionRules) bool {
	return rules.CronJobUID
}

func needDeploymentAttributes(rules ExtractionRules) bool {
	return rules.DeploymentUID
}

func needNamespaceAttributes(rules ExtractionRules) bool {
	for _, r := range rules.Labels {
		if r.From == MetadataFromNamespace {
			return true
		}
	}

	for _, r := range rules.Annotations {
		if r.From == MetadataFromNamespace {
			return true
		}
	}

	return false || needCronJobAttributes(rules) || needDeploymentAttributes(rules)
}

func needContainerAttributes(rules ExtractionRules) bool {
	return rules.ContainerImageName || rules.ContainerImageTag || rules.ContainerID
}
