package reporters

import (
	"bytes"
	"log"
	"net/http"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
)

type MetricsReporter struct {
	endpoint string
	token    string
}

func NewMetricsReporter(config *config.Config) *MetricsReporter {
	return &MetricsReporter{
		endpoint: config.LUMIGO_ENDPOINT + "/api/v1/",
		token:    config.LUMITO_TOKEN,
	}
}

func (r *MetricsReporter) Report(metrics string) {

	url := r.endpoint + "metrics"
	token := r.token

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(metrics)))
	if err != nil {
		log.Println("Error creating request:", err)
		return
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error sending request:", err)
		return
	}
	defer resp.Body.Close()

	log.Println("Metrics sent successfully:", resp.Status)
}
