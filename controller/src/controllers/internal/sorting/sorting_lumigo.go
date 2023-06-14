package sorting

import (
	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
)

// To be used with sort.Sort
type ByCreationTime []operatorv1alpha1.Lumigo

func (s ByCreationTime) Len() int {
	return len(s)
}
func (s ByCreationTime) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByCreationTime) Less(i, j int) bool {
	return s[i].CreationTimestamp.Before(&s[j].CreationTimestamp)
}
