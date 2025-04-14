package reporters

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
)

type MetricsReporter struct {
	endpoint string
	config   *config.Config
}

func NewMetricsReporter(config *config.Config) *MetricsReporter {
	return &MetricsReporter{
		endpoint: config.LUMIGO_ENDPOINT + "/api/v1/",
		config:   config,
	}
}

func (r *MetricsReporter) Report(metrics string) {
	url := r.endpoint + "metrics"
	token := r.config.LUMIGO_TOKEN

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(metrics)))
	if err != nil {
		log.Println("Error creating request:", err)
		return
	}
	encodedToken := base64.StdEncoding.EncodeToString([]byte(token))
	req.Header.Set("Authorization", fmt.Sprintf("LumigoToken %s", encodedToken))
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
