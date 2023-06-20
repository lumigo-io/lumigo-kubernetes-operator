package internal // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sdataenricherprocessor/internal"

import (
	"context"
	"fmt"
	"reflect"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	watchSyncPeriod = time.Minute * 0 // time.Minute * 5

	defaultCacheMaxEntries = 1000

	retryAttempts   = 5
	retryAttempStep = 100 * time.Millisecond
)

type KubeClient struct {
	client kubernetes.Interface
	logger *zap.Logger

	namespaceInformer cache.SharedInformer

	podCache    lruWithIndexer[corev1.Pod]
	podInformer cache.SharedInformer

	daemonSetCache    lruWithIndexer[appsv1.DaemonSet]
	daemonSetInformer cache.SharedInformer

	deploymentCache    lruWithIndexer[appsv1.Deployment]
	deploymentInformer cache.SharedInformer

	replicaSetCache    lruWithIndexer[appsv1.ReplicaSet]
	replicaSetInformer cache.SharedInformer

	statefulSetCache    lruWithIndexer[appsv1.StatefulSet]
	statefulSetInformer cache.SharedInformer

	cronJobCache    lruWithIndexer[batchv1.CronJob]
	cronJobInformer cache.SharedInformer

	jobCache    lruWithIndexer[batchv1.Job]
	jobInformer cache.SharedInformer

	// New object resource versions to be sent downstream
	ObjectResourceVersions chan runtime.Object

	// Its closing signals the k8s watchers/informers to stop watching for new events
	isStarted bool

	stopCh chan struct{}
}

// TODO Keep only info we need in the operator
type lruWithIndexer[V any] struct {
	cache     *lru.Cache[string, V]
	indexFunc func(*V) (string, string)
	// We will not store all data in the LRU cache, only what we need to answer lookup methods
	compactorFunc        func(*V) *V
	onNewResourceVersion func(*V)
}

func (l *lruWithIndexer[V]) Add(object V) bool {
	objectKey, objectResourceVersionKey := l.indexFunc(&object)
	// For eviction we check only the key with the specific resource version;
	// we expect the object-generic key to be evicted very often

	objectResourceVersionSeenAlready := l.cache.Contains(objectResourceVersionKey)
	if objectResourceVersionSeenAlready {
		return false
	}

	compactedObject := *l.compactorFunc(&object)

	l.cache.Add(objectKey, compactedObject)
	l.cache.Add(objectResourceVersionKey, compactedObject)

	// Although we use a buffered channel, which should seldom block,
	// we do not want to run the risk of deadlocks, so we put the queing
	// on a goroutine. We send the full object downstream, while we keep
	// locally only info we need to expose via public methods.
	go l.onNewResourceVersion(&object)

	return true
}

func (l *lruWithIndexer[V]) AddUntyped(object interface{}) (bool, error) {
	v, ok := object.(*V)
	if !ok {
		return false, fmt.Errorf("the received object is not of the right type: " + reflect.TypeOf(object).String())
	}

	return l.Add(*v), nil
}

func (l *lruWithIndexer[V]) Get(key string) (V, bool) {
	return l.cache.Get(key)
}

func (l *lruWithIndexer[V]) Keys() []string {
	return l.cache.Keys()
}

func New(logger *zap.Logger, client kubernetes.Interface) (*KubeClient, error) {
	// There's usually many more pods than anything else
	podLRU, err := lru.New[string, corev1.Pod](defaultCacheMaxEntries * 10)
	if err != nil {
		return nil, err
	}

	daemonSetLRU, err := lru.New[string, appsv1.DaemonSet](defaultCacheMaxEntries)
	if err != nil {
		return nil, err
	}

	deploymentLRU, err := lru.New[string, appsv1.Deployment](defaultCacheMaxEntries)
	if err != nil {
		return nil, err
	}

	replicaSetLRU, err := lru.New[string, appsv1.ReplicaSet](defaultCacheMaxEntries)
	if err != nil {
		return nil, err
	}

	statefulSetLRU, err := lru.New[string, appsv1.StatefulSet](defaultCacheMaxEntries)
	if err != nil {
		return nil, err
	}

	cronJobLRU, err := lru.New[string, batchv1.CronJob](defaultCacheMaxEntries)
	if err != nil {
		return nil, err
	}

	jobLRU, err := lru.New[string, batchv1.Job](defaultCacheMaxEntries)
	if err != nil {
		return nil, err
	}

	objectResourceVersions := make(chan runtime.Object, 200)

	return &KubeClient{
		client: client,
		logger: logger,
		namespaceInformer: cache.NewSharedInformer(
			&cache.ListWatch{
				ListFunc:  namespaceInformerListFunc(client),
				WatchFunc: namespaceInformerWatchFunc(client),
			},
			&corev1.Namespace{},
			watchSyncPeriod,
		),
		podCache: lruWithIndexer[corev1.Pod]{
			cache: podLRU,
			compactorFunc: func(o *corev1.Pod) *corev1.Pod {
				return &corev1.Pod{
					ObjectMeta: compactObjectMeta(o.ObjectMeta),
				}
			},
			indexFunc: func(o *corev1.Pod) (string, string) {
				return metadataToCacheKeys(o.ObjectMeta)
			},
			onNewResourceVersion: func(o *corev1.Pod) {
				objectResourceVersions <- o
			},
		},
		podInformer: cache.NewSharedInformer(
			&cache.ListWatch{
				ListFunc:  podInformerListFunc(client),
				WatchFunc: podInformerWatchFunc(client),
			},
			&corev1.Pod{},
			watchSyncPeriod,
		),
		daemonSetCache: lruWithIndexer[appsv1.DaemonSet]{
			cache: daemonSetLRU,
			compactorFunc: func(o *appsv1.DaemonSet) *appsv1.DaemonSet {
				return &appsv1.DaemonSet{
					ObjectMeta: compactObjectMeta(o.ObjectMeta),
				}
			},
			indexFunc: func(o *appsv1.DaemonSet) (string, string) {
				return metadataToCacheKeys(o.ObjectMeta)
			},
			onNewResourceVersion: func(o *appsv1.DaemonSet) {
				objectResourceVersions <- o
			},
		},
		daemonSetInformer: cache.NewSharedInformer(
			&cache.ListWatch{
				ListFunc:  daemonSetInformerListFunc(client),
				WatchFunc: daemonSetInformerWatchFunc(client),
			},
			&appsv1.DaemonSet{},
			watchSyncPeriod,
		),
		deploymentCache: lruWithIndexer[appsv1.Deployment]{
			cache: deploymentLRU,
			compactorFunc: func(o *appsv1.Deployment) *appsv1.Deployment {
				return &appsv1.Deployment{
					ObjectMeta: compactObjectMeta(o.ObjectMeta),
				}
			},
			indexFunc: func(o *appsv1.Deployment) (string, string) {
				return metadataToCacheKeys(o.ObjectMeta)
			},
			onNewResourceVersion: func(o *appsv1.Deployment) {
				objectResourceVersions <- o
			},
		},
		deploymentInformer: cache.NewSharedInformer(
			&cache.ListWatch{
				ListFunc:  deploymentInformerListFunc(client),
				WatchFunc: deploymentInformerWatchFunc(client),
			},
			&appsv1.Deployment{},
			watchSyncPeriod,
		),
		replicaSetCache: lruWithIndexer[appsv1.ReplicaSet]{
			cache: replicaSetLRU,
			compactorFunc: func(o *appsv1.ReplicaSet) *appsv1.ReplicaSet {
				return &appsv1.ReplicaSet{
					ObjectMeta: compactObjectMeta(o.ObjectMeta),
				}
			},
			indexFunc: func(o *appsv1.ReplicaSet) (string, string) {
				return metadataToCacheKeys(o.ObjectMeta)
			},
			onNewResourceVersion: func(o *appsv1.ReplicaSet) {
				objectResourceVersions <- o
			},
		},
		replicaSetInformer: cache.NewSharedInformer(
			&cache.ListWatch{
				ListFunc:  replicaSetInformerListFunc(client),
				WatchFunc: replicaSetInformerWatchFunc(client),
			},
			&appsv1.ReplicaSet{},
			watchSyncPeriod,
		),
		statefulSetCache: lruWithIndexer[appsv1.StatefulSet]{
			cache: statefulSetLRU,
			compactorFunc: func(o *appsv1.StatefulSet) *appsv1.StatefulSet {
				return &appsv1.StatefulSet{
					ObjectMeta: compactObjectMeta(o.ObjectMeta),
				}
			},
			indexFunc: func(o *appsv1.StatefulSet) (string, string) {
				return metadataToCacheKeys(o.ObjectMeta)
			},
			onNewResourceVersion: func(o *appsv1.StatefulSet) {
				objectResourceVersions <- o
			},
		},
		statefulSetInformer: cache.NewSharedInformer(
			&cache.ListWatch{
				ListFunc:  statefulSetInformerListFunc(client),
				WatchFunc: statefulSetInformerWatchFunc(client),
			},
			&appsv1.StatefulSet{},
			watchSyncPeriod,
		),
		cronJobCache: lruWithIndexer[batchv1.CronJob]{
			cache: cronJobLRU,
			compactorFunc: func(o *batchv1.CronJob) *batchv1.CronJob {
				return &batchv1.CronJob{
					ObjectMeta: compactObjectMeta(o.ObjectMeta),
				}
			},
			indexFunc: func(o *batchv1.CronJob) (string, string) {
				return metadataToCacheKeys(o.ObjectMeta)
			},
			onNewResourceVersion: func(o *batchv1.CronJob) {
				objectResourceVersions <- o
			},
		},
		cronJobInformer: cache.NewSharedInformer(
			&cache.ListWatch{
				ListFunc:  cronJobInformerListFunc(client),
				WatchFunc: cronJobInformerWatchFunc(client),
			},
			&batchv1.CronJob{},
			watchSyncPeriod,
		),
		jobCache: lruWithIndexer[batchv1.Job]{
			cache: jobLRU,
			compactorFunc: func(o *batchv1.Job) *batchv1.Job {
				return &batchv1.Job{
					ObjectMeta: compactObjectMeta(o.ObjectMeta),
				}
			},
			indexFunc: func(o *batchv1.Job) (string, string) {
				return metadataToCacheKeys(o.ObjectMeta)
			},
			onNewResourceVersion: func(o *batchv1.Job) {
				objectResourceVersions <- o
			},
		},
		jobInformer: cache.NewSharedInformer(
			&cache.ListWatch{
				ListFunc:  jobInformerListFunc(client),
				WatchFunc: jobInformerWatchFunc(client),
			},
			&batchv1.Job{},
			watchSyncPeriod,
		),
		ObjectResourceVersions: objectResourceVersions,
		isStarted:              false,
		stopCh:                 make(chan struct{}),
	}, nil
}

func compactObjectMeta(o metav1.ObjectMeta) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       o.Namespace,
		Name:            o.Name,
		UID:             o.UID,
		ResourceVersion: o.ResourceVersion,
		OwnerReferences: o.OwnerReferences,
	}
}

func metadataToCacheKeys(objectMeta metav1.ObjectMeta) (string, string) {
	return fmt.Sprintf("%s/%s", objectMeta.Namespace, objectMeta.Name),
		fmt.Sprintf("%s/%s@%s", objectMeta.Namespace, objectMeta.Name, objectMeta.ResourceVersion)
}

func cachedEventHandler[T any](c *lruWithIndexer[T], newObjects chan runtime.Object, logger *zap.Logger) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if _, err := c.AddUntyped(obj); err != nil {
				logger.Error(
					"Cannot store object in the cache",
					zap.Error(err),
				)
			} else {
				logger.Debug("Added new object to cache", zap.Any("object", obj))
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if evicted, err := c.AddUntyped(newObj); err != nil {
				logger.Error(
					"Cannot store object in the cache",
					zap.Error(err),
				)
			} else if evicted {
				logger.Debug("Updated object to cache", zap.Any("object", newObj))
			} else {
				logger.Debug("Updated object to cache, but no eviction occurred (maybe the update object was already pruned due to LRU?)", zap.Any("object", newObj))
			}
		},
		DeleteFunc: func(obj interface{}) {
			// Nothing to do, deletion is managed by LRU
		},
	}
}

func (c *KubeClient) Start() error {
	if c.isStarted {
		return nil
	}

	if _, err := c.namespaceInformer.AddEventHandler(createEventHandler(c.logger, "namespace")); err != nil {
		return fmt.Errorf("cannot register event handler for namespace informer: %w", err)
	}

	if _, err := c.podInformer.AddEventHandler(cachedEventHandler(&c.podCache, c.ObjectResourceVersions, c.logger)); err != nil {
		return fmt.Errorf("cannot register event handler for pod informer: %w", err)
	}

	if _, err := c.daemonSetInformer.AddEventHandler(cachedEventHandler(&c.daemonSetCache, c.ObjectResourceVersions, c.logger)); err != nil {
		return fmt.Errorf("cannot register event handler for daemonset informer: %w", err)
	}

	if _, err := c.deploymentInformer.AddEventHandler(cachedEventHandler(&c.deploymentCache, c.ObjectResourceVersions, c.logger)); err != nil {
		return fmt.Errorf("cannot register event handler for deployment informer: %w", err)
	}

	if _, err := c.replicaSetInformer.AddEventHandler(cachedEventHandler(&c.replicaSetCache, c.ObjectResourceVersions, c.logger)); err != nil {
		return fmt.Errorf("cannot register event handler for replicaset informer: %w", err)
	}

	if _, err := c.statefulSetInformer.AddEventHandler(cachedEventHandler(&c.statefulSetCache, c.ObjectResourceVersions, c.logger)); err != nil {
		return fmt.Errorf("cannot register event handler for statefulset informer: %w", err)
	}

	if _, err := c.cronJobInformer.AddEventHandler(cachedEventHandler(&c.cronJobCache, c.ObjectResourceVersions, c.logger)); err != nil {
		return fmt.Errorf("cannot register event handler for cronjob informer: %w", err)
	}

	if _, err := c.jobInformer.AddEventHandler(cachedEventHandler(&c.jobCache, c.ObjectResourceVersions, c.logger)); err != nil {
		return fmt.Errorf("cannot register event handler for job informer: %w", err)
	}

	go c.namespaceInformer.Run(c.stopCh)
	go c.podInformer.Run(c.stopCh)
	go c.daemonSetInformer.Run(c.stopCh)
	go c.deploymentInformer.Run(c.stopCh)
	go c.replicaSetInformer.Run(c.stopCh)
	go c.statefulSetInformer.Run(c.stopCh)
	go c.cronJobInformer.Run(c.stopCh)
	go c.jobInformer.Run(c.stopCh)

	c.isStarted = true
	c.logger.Debug("KubeClient started")

	return nil
}

func (c *KubeClient) Stop() {
	// Nothing to do: the Kube client does not stop when the processor utilizing it does,
	// or we loose all the caches
}

func (c *KubeClient) EnsureObjectReferenceIsKnown(objectReference corev1.ObjectReference) error {
	c.logger.Debug("Ensuring object reference is known", zap.Any("object-reference", objectReference))
	switch objectReference.GroupVersionKind() {
	case V1Pod:
		if _, err := c.getPod(objectReference.Name, objectReference.Namespace, objectReference.ResourceVersion); err != nil {
			return err
		}
	case AppsV1DaemonSet:
		if _, err := c.getDaemonSet(objectReference.Name, objectReference.Namespace, objectReference.ResourceVersion); err != nil {
			return err
		}
	case AppsV1Deployment:
		if _, err := c.getDeployment(objectReference.Name, objectReference.Namespace, objectReference.ResourceVersion); err != nil {
			return err
		}
	case AppsV1ReplicaSet:
		if _, err := c.getReplicaSet(objectReference.Name, objectReference.Namespace, objectReference.ResourceVersion); err != nil {
			return err
		}
	case AppsV1StatefulSet:
		if _, err := c.getStatefulSet(objectReference.Name, objectReference.Namespace, objectReference.ResourceVersion); err != nil {
			return err
		}
	case BatchV1CronJob:
		if _, err := c.getCronJob(objectReference.Name, objectReference.Namespace, objectReference.ResourceVersion); err != nil {
			return err
		}
	case BatchV1Job:
		if _, err := c.getJob(objectReference.Name, objectReference.Namespace, objectReference.ResourceVersion); err != nil {
			return err
		}
	default:
		c.logger.Debug("Unexpected GVK of object reference", zap.Any("object-reference", objectReference))
	}

	return nil
}

func (c *KubeClient) RegisterNamespace(namespace *corev1.Namespace) {
	registerObject(c.namespaceInformer, c.logger, "namespace", namespace)
}

func (c *KubeClient) RegisterPod(pod *corev1.Pod) {
	c.podCache.Add(*pod)
}

func (c *KubeClient) RegisterDaemonSet(daemonSet *appsv1.DaemonSet) {
	c.daemonSetCache.Add(*daemonSet)
}

func (c *KubeClient) RegisterDeployment(deployment *appsv1.Deployment) {
	c.deploymentCache.Add(*deployment)
}

func (c *KubeClient) RegisterReplicaSet(replicaSet *appsv1.ReplicaSet) {
	c.replicaSetCache.Add(*replicaSet)
}

func (c *KubeClient) RegisterStatefulSet(statefulSet *appsv1.StatefulSet) {
	c.statefulSetCache.Add(*statefulSet)
}

func (c *KubeClient) RegisterCronJob(cronJob *batchv1.CronJob) {
	c.cronJobCache.Add(*cronJob)
}

func (c *KubeClient) RegisterJob(job *batchv1.Job) {
	c.jobCache.Add(*job)
}

func registerObject(informer cache.SharedInformer, logger *zap.Logger, kind string, object runtime.Object) {
	store := informer.GetStore()
	if err := store.Add(object); err != nil {
		logger.Debug("Error while manually registering "+kind+" in "+kind+"'s store", zap.Any(kind, object), zap.Error(err))
	} else {
		logger.Debug("Manually registered "+kind+" in "+kind+"'s store", zap.Any(kind, object))
	}
}

func (c *KubeClient) GetNamespaceUID(namespaceName string) (types.UID, error) {
	// TODO Harden with retry
	if ns, exists, err := c.namespaceInformer.GetStore().GetByKey(namespaceName); err != nil {
		return types.UID(""), fmt.Errorf("cannot retrieve '%s' v1.Namespace: %w", namespaceName, err)
	} else if exists {
		if namespace, ok := ns.(*corev1.Namespace); !ok {
			return types.UID(""), fmt.Errorf("object found in the namespace cache for the '%s' key is not a v1.Namespace object: %+v", namespaceName, ns)
		} else {
			return namespace.UID, nil
		}
	} else {
		return types.UID(""), fmt.Errorf("namespace '%s' not found", namespaceName)
	}
}

func (c *KubeClient) GetCronJobUID(cronJobName, namespaceName string) (types.UID, error) {
	if cronJob, err := c.getCronJob(cronJobName, namespaceName, ""); err != nil {
		return types.UID(""), err
	} else {
		return cronJob.UID, nil
	}
}

func (c *KubeClient) GetDeploymentUID(deploymentName, namespaceName string) (types.UID, error) {
	if deployment, err := c.getDeployment(deploymentName, namespaceName, ""); err != nil {
		return types.UID(""), err
	} else {
		return deployment.UID, nil
	}
}

func namespaceInformerListFunc(client kubernetes.Interface) cache.ListFunc {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		return client.CoreV1().Namespaces().List(context.Background(), opts)
	}
}

func namespaceInformerWatchFunc(client kubernetes.Interface) cache.WatchFunc {
	return func(opts metav1.ListOptions) (watch.Interface, error) {
		return client.CoreV1().Namespaces().Watch(context.Background(), opts)
	}
}

func podInformerListFunc(client kubernetes.Interface) cache.ListFunc {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		return client.CoreV1().Pods("").List(context.Background(), opts)
	}
}

func podInformerWatchFunc(client kubernetes.Interface) cache.WatchFunc {
	return func(opts metav1.ListOptions) (watch.Interface, error) {
		return client.CoreV1().Pods("").Watch(context.Background(), opts)
	}
}

func daemonSetInformerListFunc(client kubernetes.Interface) cache.ListFunc {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		return client.AppsV1().DaemonSets("").List(context.Background(), opts)
	}
}

func daemonSetInformerWatchFunc(client kubernetes.Interface) cache.WatchFunc {
	return func(opts metav1.ListOptions) (watch.Interface, error) {
		return client.AppsV1().DaemonSets("").Watch(context.Background(), opts)
	}
}

func deploymentInformerListFunc(client kubernetes.Interface) cache.ListFunc {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		return client.AppsV1().Deployments("").List(context.Background(), opts)
	}
}

func deploymentInformerWatchFunc(client kubernetes.Interface) cache.WatchFunc {
	return func(opts metav1.ListOptions) (watch.Interface, error) {
		return client.AppsV1().Deployments("").Watch(context.Background(), opts)
	}
}

func replicaSetInformerListFunc(client kubernetes.Interface) cache.ListFunc {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		return client.AppsV1().ReplicaSets("").List(context.Background(), opts)
	}
}

func replicaSetInformerWatchFunc(client kubernetes.Interface) cache.WatchFunc {
	return func(opts metav1.ListOptions) (watch.Interface, error) {
		return client.AppsV1().ReplicaSets("").Watch(context.Background(), opts)
	}
}

func statefulSetInformerListFunc(client kubernetes.Interface) cache.ListFunc {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		return client.AppsV1().StatefulSets("").List(context.Background(), opts)
	}
}

func statefulSetInformerWatchFunc(client kubernetes.Interface) cache.WatchFunc {
	return func(opts metav1.ListOptions) (watch.Interface, error) {
		return client.AppsV1().StatefulSets("").Watch(context.Background(), opts)
	}
}

func cronJobInformerListFunc(client kubernetes.Interface) cache.ListFunc {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		return client.BatchV1().CronJobs("").List(context.Background(), opts)
	}
}

func cronJobInformerWatchFunc(client kubernetes.Interface) cache.WatchFunc {
	return func(opts metav1.ListOptions) (watch.Interface, error) {
		return client.BatchV1().CronJobs("").Watch(context.Background(), opts)
	}
}

func jobInformerListFunc(client kubernetes.Interface) cache.ListFunc {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		return client.BatchV1().Jobs("").List(context.Background(), opts)
	}
}

func jobInformerWatchFunc(client kubernetes.Interface) cache.WatchFunc {
	return func(opts metav1.ListOptions) (watch.Interface, error) {
		return client.BatchV1().Jobs("").Watch(context.Background(), opts)
	}
}

func createEventHandler(logger *zap.Logger, kind string) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			logger.Debug("Received new "+kind, zap.Any(kind, obj))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			logger.Debug("Updated "+kind, zap.Any(kind, newObj))
		},
		DeleteFunc: func(obj interface{}) {
			logger.Debug("Deleted "+kind, zap.Any(kind, obj))
		},
	}
}

func (c *KubeClient) GetRootOwnerReference(ctx context.Context, event *corev1.Event) (map[string]interface{}, error) {
	currentObjectReference := &event.InvolvedObject
	for currentObjectReference != nil {
		if ownerReference, err := c.getFirstRelevantOwnerReferenceAsObjectReference(ctx, currentObjectReference); err != nil {
			return nil, fmt.Errorf("cannot retrieve owner reference of object reference %v: %w", currentObjectReference, err)
		} else if ownerReference != nil {
			currentObjectReference = ownerReference
		} else {
			/*
			 * The object pointed at by the currentObjectReference does not have an owner reference,
			 * so it is our root.
			 */
			break
		}
	}

	// Marshal currentObjectReference, if it exists, as root owner reference
	if currentObjectReference != nil {
		return runtime.DefaultUnstructuredConverter.ToUnstructured(currentObjectReference)
	}

	return nil, nil
}

func (c *KubeClient) getFirstRelevantOwnerReferenceAsObjectReference(ctx context.Context, objectReference *corev1.ObjectReference) (*corev1.ObjectReference, error) {
	/*
	 * Check via GVK if the object pointed at by the object reference is relevant for us and,
	 * if so, retrieve the object, and return an object reference to the first owner reference
	 * that has an applicable GVK.
	 */
	objRefGvk := objectReference.GroupVersionKind()

	var ownerReferences []metav1.OwnerReference
	switch objRefGvk {
	case V1Pod:
		{
			if pod, err := c.getPod(objectReference.Name, objectReference.Namespace, ""); err != nil {
				return nil, err
			} else {
				ownerReferences = pod.OwnerReferences
			}
		}
	case AppsV1DaemonSet:
		{
			if daemonset, err := c.getDaemonSet(objectReference.Name, objectReference.Namespace, ""); err != nil {
				return nil, err
			} else {
				ownerReferences = daemonset.OwnerReferences
			}
		}
	case AppsV1Deployment:
		{
			if deployment, err := c.getDeployment(objectReference.Name, objectReference.Namespace, ""); err != nil {
				return nil, err
			} else {
				ownerReferences = deployment.OwnerReferences
			}
		}
	case AppsV1ReplicaSet:
		{
			if replicaSet, err := c.getReplicaSet(objectReference.Name, objectReference.Namespace, ""); err != nil {
				return nil, err
			} else {
				ownerReferences = replicaSet.OwnerReferences
			}
		}
	case AppsV1StatefulSet:
		{
			if statefulSet, err := c.getStatefulSet(objectReference.Name, objectReference.Namespace, ""); err != nil {
				return nil, err
			} else {
				ownerReferences = statefulSet.OwnerReferences
			}
		}
	case BatchV1CronJob:
		{
			if cronJob, err := c.getCronJob(objectReference.Name, objectReference.Namespace, ""); err != nil {
				return nil, err
			} else {
				ownerReferences = cronJob.OwnerReferences
			}
		}
	case BatchV1Job:
		{
			if job, err := c.getJob(objectReference.Name, objectReference.Namespace, ""); err != nil {
				return nil, err
			} else {
				ownerReferences = job.OwnerReferences
			}
		}
	default:
		{
			// GVK of the object reference is not relevant
			return nil, nil
		}
	}

	for _, ownerReference := range ownerReferences {
		ownerReferenceGvk := schema.FromAPIVersionAndKind(ownerReference.APIVersion, ownerReference.Kind)

		if isGVKRelevant(ownerReferenceGvk) {
			return &corev1.ObjectReference{
				APIVersion: ownerReference.APIVersion,
				Kind:       ownerReference.Kind,
				// Owner references are always in the same namespace as the owned object
				Namespace: objectReference.Namespace,
				Name:      ownerReference.Name,
				UID:       ownerReference.UID,
			}, nil
		}
	}

	return nil, nil
}

func (c *KubeClient) getPod(name, namespace, resourceVersion string) (*corev1.Pod, error) {
	var cacheKey string
	if resourceVersion == "" {
		cacheKey = fmt.Sprintf("%s/%s", namespace, name)
	} else {
		cacheKey = fmt.Sprintf("%s/%s@%s", namespace, name, resourceVersion)
	}

	if pod, found := c.podCache.Get(cacheKey); found {
		return &pod, nil
	}

	// Try fetch from the Kubernetes API then
	c.logger.Debug(
		"Cannot find pod in local cache, looking remotely",
		zap.String("name", name),
		zap.String("namespace", namespace),
		zap.String("resourceVersion", resourceVersion),
		zap.Any("known-keys", c.podCache.Keys()),
	)

	if pod, err := retry(
		fmt.Sprintf("Get v1.Pod '%s' from the Kube API", cacheKey),
		retryAttempts,
		retryAttempStep,
		func() (*corev1.Pod, error) {
			return c.client.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{
				ResourceVersion: resourceVersion,
			})
		},
		c.logger,
	); err == nil {
		c.RegisterPod(pod)
		return pod, nil
	}

	return nil, fmt.Errorf("no '%s/%s' pod found in the local cache or in Kube API", namespace, name)
}

func (c *KubeClient) getDaemonSet(name, namespace, resourceVersion string) (*appsv1.DaemonSet, error) {
	var cacheKey string
	if resourceVersion == "" {
		cacheKey = fmt.Sprintf("%s/%s", namespace, name)
	} else {
		cacheKey = fmt.Sprintf("%s/%s@%s", namespace, name, resourceVersion)
	}

	store := c.daemonSetInformer.GetStore()
	if object, found, err := store.GetByKey(cacheKey); err != nil {
		return nil, err
	} else if found {
		if daemonSet, isOk := object.(*appsv1.DaemonSet); isOk {
			return daemonSet, nil
		} else {
			return nil, fmt.Errorf("object returned key '%s' from the local cache is not a apps/v1.DaemonSet: %v", cacheKey, object)
		}
	}

	// Try fetch from the Kubernetes API then
	c.logger.Debug(
		"Cannot find daemonset in local cache, looking remotely",
		zap.String("name", name),
		zap.String("namespace", namespace),
		zap.String("resourceVersion", resourceVersion),
		zap.Any("known-keys", store.ListKeys()),
	)

	if daemonSet, err := retry(
		fmt.Sprintf("Get apps/v1.DaemonSet '%s' from the Kube API", cacheKey),
		retryAttempts,
		retryAttempStep,
		func() (*appsv1.DaemonSet, error) {
			return c.client.AppsV1().DaemonSets(namespace).Get(context.Background(), name, metav1.GetOptions{
				ResourceVersion: resourceVersion,
			})
		},
		c.logger,
	); err == nil {
		c.RegisterDaemonSet(daemonSet)
		return daemonSet, nil
	}

	return nil, fmt.Errorf("no '%s/%s' daemonset found in the local cache or in Kube API", namespace, name)
}

func (c *KubeClient) getDeployment(name, namespace, resourceVersion string) (*appsv1.Deployment, error) {
	var cacheKey string
	if resourceVersion == "" {
		cacheKey = fmt.Sprintf("%s/%s", namespace, name)
	} else {
		cacheKey = fmt.Sprintf("%s/%s@%s", namespace, name, resourceVersion)
	}

	if deployment, found := c.deploymentCache.Get(cacheKey); found {
		return &deployment, nil
	}

	// Try fetch from the Kubernetes API then
	c.logger.Debug(
		"Cannot find deployment in local cache, looking remotely",
		zap.String("name", name),
		zap.String("namespace", namespace),
		zap.String("resourceVersion", resourceVersion),
		zap.Any("known-keys", c.deploymentCache.Keys()),
	)

	if deployment, err := retry(
		fmt.Sprintf("Get apps/v1.Deployment '%s' from the Kube API", cacheKey),
		retryAttempts,
		retryAttempStep,
		func() (*appsv1.Deployment, error) {
			return c.client.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{
				ResourceVersion: resourceVersion,
			})
		},
		c.logger,
	); err == nil {
		c.RegisterDeployment(deployment)
		return deployment, nil
	}

	return nil, fmt.Errorf("no '%s/%s' deployment found in the local cache or in Kube API", namespace, name)
}

func (c *KubeClient) getReplicaSet(name, namespace, resourceVersion string) (*appsv1.ReplicaSet, error) {
	var cacheKey string
	if resourceVersion == "" {
		cacheKey = fmt.Sprintf("%s/%s", namespace, name)
	} else {
		cacheKey = fmt.Sprintf("%s/%s@%s", namespace, name, resourceVersion)
	}

	if replicaSet, found := c.replicaSetCache.Get(cacheKey); found {
		return &replicaSet, nil
	}

	// Try fetch from the Kubernetes API then
	c.logger.Debug(
		"Cannot find replicaset in local cache, looking remotely",
		zap.String("name", name),
		zap.String("namespace", namespace),
		zap.String("resourceVersion", resourceVersion),
		zap.Any("known-keys", c.replicaSetCache.Keys()),
	)

	if replicaSet, err := retry(
		fmt.Sprintf("Get apps/v1.ReplicaSet '%s' from the Kube API", cacheKey),
		retryAttempts,
		retryAttempStep,
		func() (*appsv1.ReplicaSet, error) {
			return c.client.AppsV1().ReplicaSets(namespace).Get(context.Background(), name, metav1.GetOptions{
				ResourceVersion: resourceVersion,
			})
		},
		c.logger,
	); err == nil {
		c.RegisterReplicaSet(replicaSet)
		return replicaSet, nil
	}

	return nil, fmt.Errorf("no '%s/%s' replicaset found in the local cache or in Kube API", namespace, name)
}

func (c *KubeClient) getStatefulSet(name, namespace, resourceVersion string) (*appsv1.StatefulSet, error) {
	var cacheKey string
	if resourceVersion == "" {
		cacheKey = fmt.Sprintf("%s/%s", namespace, name)
	} else {
		cacheKey = fmt.Sprintf("%s/%s@%s", namespace, name, resourceVersion)
	}

	store := c.statefulSetInformer.GetStore()
	if object, found, err := store.GetByKey(cacheKey); err != nil {
		return nil, err
	} else if found {
		if statefulSet, isOk := object.(*appsv1.StatefulSet); isOk {
			return statefulSet, nil
		} else {
			return nil, fmt.Errorf("object returned key '%s' from the local cache is not a apps/v1.StatefulSet: %v", cacheKey, object)
		}
	}

	// Try fetch from the Kubernetes API then
	c.logger.Debug(
		"Cannot find statefulset in local cache, looking remotely",
		zap.String("name", name),
		zap.String("namespace", namespace),
		zap.String("resourceVersion", resourceVersion),
		zap.Any("known-keys", store.ListKeys()),
	)

	if statefulSet, err := retry(
		fmt.Sprintf("Get apps/v1.StatefulSet '%s' from the Kube API", cacheKey),
		retryAttempts,
		retryAttempStep,
		func() (*appsv1.StatefulSet, error) {
			return c.client.AppsV1().StatefulSets(namespace).Get(context.Background(), name, metav1.GetOptions{
				ResourceVersion: resourceVersion,
			})
		},
		c.logger,
	); err == nil {
		c.RegisterStatefulSet(statefulSet)
		return statefulSet, nil
	}

	return nil, fmt.Errorf("no '%s/%s' statefulset found in the local cache or in Kube API", namespace, name)
}

func (c *KubeClient) getCronJob(name, namespace, resourceVersion string) (*batchv1.CronJob, error) {
	var cacheKey string
	if resourceVersion == "" {
		cacheKey = fmt.Sprintf("%s/%s", namespace, name)
	} else {
		cacheKey = fmt.Sprintf("%s/%s@%s", namespace, name, resourceVersion)
	}

	store := c.cronJobInformer.GetStore()
	if object, found, err := store.GetByKey(cacheKey); err != nil {
		return nil, err
	} else if found {
		if cronJob, isOk := object.(*batchv1.CronJob); isOk {
			return cronJob, nil
		} else {
			return nil, fmt.Errorf("object returned key '%s' from the local cache is not a batchv1.CronJob: %v", cacheKey, object)
		}
	}

	// Try fetch from the Kubernetes API then
	c.logger.Debug(
		"Cannot find cronjob in local cache, looking remotely",
		zap.String("name", name),
		zap.String("namespace", namespace),
		zap.String("resourceVersion", resourceVersion),
		zap.Any("known-keys", store.ListKeys()),
	)

	if cronJob, err := retry(
		fmt.Sprintf("Get batch/v1.CronJob '%s' from the Kube API", cacheKey),
		retryAttempts,
		retryAttempStep,
		func() (*batchv1.CronJob, error) {
			return c.client.BatchV1().CronJobs(namespace).Get(context.Background(), name, metav1.GetOptions{
				ResourceVersion: resourceVersion,
			})
		},
		c.logger,
	); err == nil {
		c.RegisterCronJob(cronJob)
		return cronJob, nil
	}

	return nil, fmt.Errorf("no '%s/%s' cronjob found in the local cache or in Kube API", namespace, name)
}

func (c *KubeClient) getJob(name, namespace, resourceVersion string) (*batchv1.Job, error) {
	var cacheKey string
	if resourceVersion == "" {
		cacheKey = fmt.Sprintf("%s/%s", namespace, name)
	} else {
		cacheKey = fmt.Sprintf("%s/%s@%s", namespace, name, resourceVersion)
	}

	store := c.jobInformer.GetStore()
	if object, found, err := store.GetByKey(cacheKey); err != nil {
		return nil, err
	} else if found {
		if job, isOk := object.(*batchv1.Job); isOk {
			return job, nil
		} else {
			return nil, fmt.Errorf("object returned key '%s' from the local cache is not a batchv1.Job: %v", cacheKey, object)
		}
	}

	// Try fetch from the Kubernetes API then
	c.logger.Debug(
		"Cannot find job in local cache, looking remotely",
		zap.String("name", name),
		zap.String("namespace", namespace),
		zap.String("resourceVersion", resourceVersion),
		zap.Any("known-keys", store.ListKeys()),
	)

	if job, err := retry(
		fmt.Sprintf("Get batch/v1.Job '%s' from the Kube API", cacheKey),
		retryAttempts,
		retryAttempStep,
		func() (*batchv1.Job, error) {
			return c.client.BatchV1().Jobs(namespace).Get(context.Background(), name, metav1.GetOptions{
				ResourceVersion: resourceVersion,
			})
		},
		c.logger,
	); err == nil {
		c.RegisterJob(job)
		return job, nil
	}

	return nil, fmt.Errorf("no '%s/%s' job found in the local cache or in Kube API", namespace, name)
}

func retry[T any](desc string, maxAttempts int, sleep time.Duration, f func() (T, error), logger *zap.Logger) (result T, err error) {
	for i := 0; i < maxAttempts; i++ {
		if i > 0 {
			// TODO Add short-circuit filter for error "resource version too old"
			time.Sleep(sleep)
			sleep *= 2
		}

		now := time.Now()
		if result, err = f(); err == nil {
			logger.Debug(desc, zap.Bool("success", true), zap.Any("result", result), zap.Int("successful-attempt", i), zap.Time("successful-attempt-timestamp", now))
			return result, nil
		}
	}

	logger.Debug(desc, zap.Int("attempt", maxAttempts), zap.Bool("success", false), zap.Error(err))
	return result, fmt.Errorf("failed after %d attempts, last error: %w", maxAttempts, err)
}

func isGVKRelevant(gvk schema.GroupVersionKind) bool {
	for _, validGvk := range relevantOwnerReferenceGroupVersionKinds {
		if validGvk == gvk {
			return true
		}
	}

	return false
}
