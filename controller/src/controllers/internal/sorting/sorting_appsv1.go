package sorting

import (
	appsv1 "k8s.io/api/apps/v1"
)

type ByDaemonsetName []appsv1.DaemonSet

func (s ByDaemonsetName) Len() int {
	return len(s)
}
func (s ByDaemonsetName) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByDaemonsetName) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

type ByDeploymentName []appsv1.Deployment

func (s ByDeploymentName) Len() int {
	return len(s)
}
func (s ByDeploymentName) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByDeploymentName) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

type ByReplicaSetName []appsv1.ReplicaSet

func (s ByReplicaSetName) Len() int {
	return len(s)
}
func (s ByReplicaSetName) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByReplicaSetName) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

type ByStatefulSetName []appsv1.StatefulSet

func (s ByStatefulSetName) Len() int {
	return len(s)
}
func (s ByStatefulSetName) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByStatefulSetName) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}
