package reporters

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"log"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type KubeReporter struct {
	eventBatch   []Event
	batchMaxSize int
	timer        *time.Ticker
	endpoint     string
	config       *config.Config
}

type Event struct {
	Name              string       `json:"name"`
	Namespace         string       `json:"namespace"`
	Reason            string       `json:"reason"`
	Message           string       `json:"message"`
	FirstTimestamp    metav1.Time  `json:"firstTimestamp"`
	LastTimestamp     metav1.Time  `json:"lastTimestamp"`
	Type              string       `json:"type"`
	UID               string       `json:"uid"`
	DeletionTimestamp *metav1.Time `json:"deletionTimestamp,omitempty"`
	CreationTimestamp metav1.Time  `json:"creationTimestamp"`
	APIVersion        string       `json:"apiVersion"`
	Kind              string       `json:"kind"`
}

func NewKubeReporter(config *config.Config) *KubeReporter {
	return &KubeReporter{
		eventBatch:   []Event{},
		batchMaxSize: config.MAX_BATCH_SIZE,
		timer:        time.NewTicker(time.Duration(config.KUBE_INTERVAL) * time.Second),
		endpoint:     config.LUMIGO_ENDPOINT + "/api/v1/",
		config:       config,
	}
}

func (r *KubeReporter) Start() {
	go r.processBatch()
}

func (r *KubeReporter) AddEvent(event coreV1.Event) {
	// Convert watchers.Event to reporters.Event
	involvedObject := event.InvolvedObject
	convertedEvent := Event{
		Name:              event.Name,
		Namespace:         event.Namespace,
		Reason:            event.Reason,
		Message:           event.Message,
		FirstTimestamp:    event.FirstTimestamp,
		LastTimestamp:     event.LastTimestamp,
		Type:              event.Type,
		UID:               string(event.UID), // Convert event.UID to string
		DeletionTimestamp: event.DeletionTimestamp,
		CreationTimestamp: event.CreationTimestamp,
		APIVersion:        involvedObject.APIVersion,
		Kind:              involvedObject.Kind,
	}

	r.eventBatch = append(r.eventBatch, convertedEvent)

	if len(r.eventBatch) >= r.batchMaxSize {
		r.sendBatch()
	}
}

func (r *KubeReporter) processBatch() {
	for range r.timer.C {
		if len(r.eventBatch) > 0 {
			r.sendBatch()
		}
	}
}

func (r *KubeReporter) sendBatch() {
	// Send the batch to the backend
	// print the batch
	log.Printf("Sending batch of %d events", len(r.eventBatch))

	url := r.endpoint + "events"
	token := r.config.LUMITO_TOKEN

	eventBatchJSON, err := json.Marshal(r.eventBatch)
	if err != nil {
		log.Println("Error marshaling event batch:", err)
		return
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(eventBatchJSON))
	if err != nil {
		log.Println("Error creating request:", err)
		return
	}
	encodedToken := base64.StdEncoding.EncodeToString([]byte(token))

	req.Header.Set("Authorization", "Bearer "+encodedToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error sending request:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("Error sending request:", resp.Status)
		return
	}

	log.Println("Metrics sent successfully:", resp.Status)
	r.eventBatch = []Event{} // Reset the batch after sending
}
