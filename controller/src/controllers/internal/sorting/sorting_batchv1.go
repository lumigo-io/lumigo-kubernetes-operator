package sorting

import (
	batchv1 "k8s.io/api/batch/v1"
)

type ByCronJobName []batchv1.CronJob

func (s ByCronJobName) Len() int {
	return len(s)
}
func (s ByCronJobName) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByCronJobName) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

type ByJobName []batchv1.Job

func (s ByJobName) Len() int {
	return len(s)
}
func (s ByJobName) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByJobName) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}
