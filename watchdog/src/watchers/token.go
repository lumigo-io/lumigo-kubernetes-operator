package watchers

import (
	"context"
	"encoding/base64"
	"log"
	"path/filepath"
	"time"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type TokenWatcher struct {
	clientset     *kubernetes.Clientset
	dynamicClient dynamic.Interface
	namespace     string
	config        *config.Config
}

func NewTokenWatcher(config *config.Config) (*TokenWatcher, error) {
	k8sConfig, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	if err != nil {
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}

	return &TokenWatcher{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		namespace:     config.NAMESPACE,
		config:        config,
	}, nil
}

func (w *TokenWatcher) Watch() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		w.checkNamespaces()
		// stop the watcher if the token was set
		if w.config.LUMIGO_TOKEN != "" {
			return
		}
	}
}

func (w *TokenWatcher) checkNamespaces() {
	namespaces, err := w.clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Printf("Error listing namespaces: %s\n", err.Error())
		return
	}

	for _, ns := range namespaces.Items {
		w.checkNamespace(&ns)
	}
}

func (w *TokenWatcher) checkNamespace(ns *coreV1.Namespace) {
	// Create the unstructured object with the desired YAML content for Lumigo custom resource
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operator.lumigo.io/v1alpha1",
			"kind":       "Lumigo",
			"metadata": map[string]interface{}{
				"name": "lumigo-sample",
			},
			"spec": map[string]interface{}{
				"lumigoToken": map[string]interface{}{
					"secretRef": map[string]interface{}{
						"name": "lumigo-secret",
						"key":  "lumigo-token-key",
					},
				},
				"infrastructure": map[string]interface{}{
					"enabled": true,
					"kubeEvents": map[string]interface{}{
						"enabled": true,
					},
				},
				"tracing": map[string]interface{}{
					"injection": map[string]interface{}{
						"enabled": true,
						"injectLumigoIntoExistingResourcesOnCreation": true,
						"removeLumigoFromResourcesOnDeletion":         true,
					},
				},
			},
		},
	}

	// Set the GroupVersionKind for the object
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "operator.lumigo.io",
		Version: "v1alpha1",
		Kind:    "Lumigo",
	})

	// Use the dynamic client to create custom resource
	list, err := w.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "operator.lumigo.io",
		Version:  "v1alpha1",
		Resource: "lumigoes",
	}).Namespace(ns.Name).List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		log.Printf("Error listing custom resources in namespace %s: %v\n", ns.Name, err)
		return
	}

	for _, item := range list.Items {
		// Get the secret token key from the custom resource
		secretRef := item.Object["spec"].(map[string]interface{})["lumigoToken"].(map[string]interface{})["secretRef"].(map[string]interface{})["key"].(string)
		keyRef := item.Object["spec"].(map[string]interface{})["lumigoToken"].(map[string]interface{})["secretRef"].(map[string]interface{})["name"].(string)
		secretValue, err := w.GetToken(ns.Name, keyRef, secretRef)
		if err != nil {
			log.Printf("Error getting token in namespace %s: %v\n", ns.Name, err)
		} else {
			w.config.SetToken(secretValue)
			log.Println("Token was set. Stopping the watcher for token...")
			return
		}
	}
}

func (w *TokenWatcher) GetToken(namespace string, secret string, key string) (string, error) {
	// Use the dynamic client to get the secret token
	secretObj, err := w.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}).Namespace(namespace).Get(context.TODO(), secret, metav1.GetOptions{})

	if err != nil {
		log.Printf("Error getting secret: %v\n", err)
		return "", err
	}

	// Get the token from the secret
	token := secretObj.Object["data"].(map[string]interface{})[key].(string)

	// Decode the token
	decodedToken, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		log.Printf("Error decoding token: %v\n", err)
		return "", err
	}
	return string(decodedToken), nil
}
