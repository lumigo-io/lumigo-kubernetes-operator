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

package observability // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sattributesprocessor/internal/observability"

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
)

// TODO: re-think if processor should register it's own telemetry views or if some other
// mechanism should be used by the collector to discover views from all components

func init() {
	_ = view.Register(
		viewPodsUpdated,
		viewPodsAdded,
		viewPodsDeleted,
		viewIPLookupMiss,
		viewPodTableSize,
		viewDeploymentsUpdated,
		viewDeploymentsAdded,
		viewDeploymentsDeleted,
		viewDeploymentTableSize,
		viewCronJobsUpdated,
		viewCronJobsAdded,
		viewCronJobsDeleted,
		viewCronJobTableSize,
		viewNamespacesAdded,
		viewNamespacesUpdated,
		viewNamespacesDeleted,
	)
}

var (
	mPodsUpdated         = stats.Int64("otelsvc/k8s/pod_updated", "Number of pod update events received", "1")
	mPodsAdded           = stats.Int64("otelsvc/k8s/pod_added", "Number of pod add events received", "1")
	mPodsDeleted         = stats.Int64("otelsvc/k8s/pod_deleted", "Number of pod delete events received", "1")
	mPodTableSize        = stats.Int64("otelsvc/k8s/pod_table_size", "Size of table containing pod info", "1")
	mIPLookupMiss        = stats.Int64("otelsvc/k8s/ip_lookup_miss", "Number of times pod by IP lookup failed.", "1")
	mDeploymentsUpdated  = stats.Int64("otelsvc/k8s/deployment_updated", "Number of deployment update events received", "1")
	mDeploymentsAdded    = stats.Int64("otelsvc/k8s/deployment_added", "Number of deployment add events received", "1")
	mDeploymentsDeleted  = stats.Int64("otelsvc/k8s/deployment_deleted", "Number of deployment delete events received", "1")
	mDeploymentTableSize = stats.Int64("otelsvc/k8s/deployment_table_size", "Size of table containing deployment info", "1")
	mCronJobsUpdated     = stats.Int64("otelsvc/k8s/cronjob_updated", "Number of cronjob update events received", "1")
	mCronJobsAdded       = stats.Int64("otelsvc/k8s/cronjob_added", "Number of cronjob add events received", "1")
	mCronJobsDeleted     = stats.Int64("otelsvc/k8s/cronjob_deleted", "Number of cronjob delete events received", "1")
	mCronJobTableSize    = stats.Int64("otelsvc/k8s/cronjob_table_size", "Size of table containing cronjob info", "1")
	mNamespacesUpdated   = stats.Int64("otelsvc/k8s/namespace_updated", "Number of namespace update events received", "1")
	mNamespacesAdded     = stats.Int64("otelsvc/k8s/namespace_added", "Number of namespace add events received", "1")
	mNamespacesDeleted   = stats.Int64("otelsvc/k8s/namespace_deleted", "Number of namespace delete events received", "1")
)

var viewPodsUpdated = &view.View{
	Name:        mPodsUpdated.Name(),
	Description: mPodsUpdated.Description(),
	Measure:     mPodsUpdated,
	Aggregation: view.Sum(),
}

var viewPodsAdded = &view.View{
	Name:        mPodsAdded.Name(),
	Description: mPodsAdded.Description(),
	Measure:     mPodsAdded,
	Aggregation: view.Sum(),
}

var viewPodsDeleted = &view.View{
	Name:        mPodsDeleted.Name(),
	Description: mPodsDeleted.Description(),
	Measure:     mPodsDeleted,
	Aggregation: view.Sum(),
}

var viewIPLookupMiss = &view.View{
	Name:        mIPLookupMiss.Name(),
	Description: mIPLookupMiss.Description(),
	Measure:     mIPLookupMiss,
	Aggregation: view.Sum(),
}

var viewPodTableSize = &view.View{
	Name:        mPodTableSize.Name(),
	Description: mPodTableSize.Description(),
	Measure:     mPodTableSize,
	Aggregation: view.LastValue(),
}

var viewDeploymentsUpdated = &view.View{
	Name:        mDeploymentsUpdated.Name(),
	Description: mDeploymentsUpdated.Description(),
	Measure:     mDeploymentsUpdated,
	Aggregation: view.Sum(),
}

var viewDeploymentsAdded = &view.View{
	Name:        mDeploymentsAdded.Name(),
	Description: mDeploymentsAdded.Description(),
	Measure:     mDeploymentsAdded,
	Aggregation: view.Sum(),
}

var viewDeploymentsDeleted = &view.View{
	Name:        mDeploymentsDeleted.Name(),
	Description: mDeploymentsDeleted.Description(),
	Measure:     mDeploymentsDeleted,
	Aggregation: view.Sum(),
}

var viewDeploymentTableSize = &view.View{
	Name:        mDeploymentTableSize.Name(),
	Description: mDeploymentTableSize.Description(),
	Measure:     mDeploymentTableSize,
	Aggregation: view.LastValue(),
}

var viewCronJobsUpdated = &view.View{
	Name:        mCronJobsUpdated.Name(),
	Description: mCronJobsUpdated.Description(),
	Measure:     mCronJobsUpdated,
	Aggregation: view.Sum(),
}

var viewCronJobsAdded = &view.View{
	Name:        mCronJobsAdded.Name(),
	Description: mCronJobsAdded.Description(),
	Measure:     mCronJobsAdded,
	Aggregation: view.Sum(),
}

var viewCronJobsDeleted = &view.View{
	Name:        mCronJobsDeleted.Name(),
	Description: mCronJobsDeleted.Description(),
	Measure:     mCronJobsDeleted,
	Aggregation: view.Sum(),
}

var viewCronJobTableSize = &view.View{
	Name:        mCronJobTableSize.Name(),
	Description: mCronJobTableSize.Description(),
	Measure:     mCronJobTableSize,
	Aggregation: view.LastValue(),
}

var viewNamespacesUpdated = &view.View{
	Name:        mNamespacesUpdated.Name(),
	Description: mNamespacesUpdated.Description(),
	Measure:     mNamespacesUpdated,
	Aggregation: view.Sum(),
}

var viewNamespacesAdded = &view.View{
	Name:        mNamespacesAdded.Name(),
	Description: mNamespacesAdded.Description(),
	Measure:     mNamespacesAdded,
	Aggregation: view.Sum(),
}

var viewNamespacesDeleted = &view.View{
	Name:        mNamespacesDeleted.Name(),
	Description: mNamespacesDeleted.Description(),
	Measure:     mNamespacesDeleted,
	Aggregation: view.Sum(),
}

// RecordPodUpdated increments the metric that records pod update events received.
func RecordPodUpdated() {
	stats.Record(context.Background(), mPodsUpdated.M(int64(1)))
}

// RecordPodAdded increments the metric that records pod add events receiver.
func RecordPodAdded() {
	stats.Record(context.Background(), mPodsAdded.M(int64(1)))
}

// RecordPodDeleted increments the metric that records pod events deleted.
func RecordPodDeleted() {
	stats.Record(context.Background(), mPodsDeleted.M(int64(1)))
}

// RecordIPLookupMiss increments the metric that records Pod lookup by IP misses.
func RecordIPLookupMiss() {
	stats.Record(context.Background(), mIPLookupMiss.M(int64(1)))
}

// RecordPodTableSize store size of pod table field in WatchClient
func RecordPodTableSize(podTableSize int64) {
	stats.Record(context.Background(), mPodTableSize.M(podTableSize))
}

// RecordDeploymentUpdated increments the metric that records deployment update events received.
func RecordDeploymentUpdated() {
	stats.Record(context.Background(), mDeploymentsUpdated.M(int64(1)))
}

// RecordDeploymentAdded increments the metric that records deployment add events receiver.
func RecordDeploymentAdded() {
	stats.Record(context.Background(), mDeploymentsAdded.M(int64(1)))
}

// RecordDeploymentDeleted increments the metric that records deployment events deleted.
func RecordDeploymentDeleted() {
	stats.Record(context.Background(), mDeploymentsDeleted.M(int64(1)))
}

// RecordDeploymentTableSize store size of deployment table field in WatchClient
func RecordDeploymentTableSize(podTableSize int64) {
	stats.Record(context.Background(), mDeploymentTableSize.M(podTableSize))
}

// RecordCronJobUpdated increments the metric that records cronjob update events received.
func RecordCronJobUpdated() {
	stats.Record(context.Background(), mCronJobsUpdated.M(int64(1)))
}

// RecordCronJobAdded increments the metric that records cronjob add events receiver.
func RecordCronJobAdded() {
	stats.Record(context.Background(), mCronJobsAdded.M(int64(1)))
}

// RecordCronJobDeleted increments the metric that records cronjob events deleted.
func RecordCronJobDeleted() {
	stats.Record(context.Background(), mCronJobsDeleted.M(int64(1)))
}

// RecordCronJobTableSize store size of cronjob table field in WatchClient
func RecordCronJobTableSize(podTableSize int64) {
	stats.Record(context.Background(), mCronJobTableSize.M(podTableSize))
}

// RecordNamespaceUpdated increments the metric that records namespace update events received.
func RecordNamespaceUpdated() {
	stats.Record(context.Background(), mNamespacesUpdated.M(int64(1)))
}

// RecordNamespaceAdded increments the metric that records namespace add events receiver.
func RecordNamespaceAdded() {
	stats.Record(context.Background(), mNamespacesAdded.M(int64(1)))
}

// RecordNamespaceDeleted increments the metric that records namespace events deleted.
func RecordNamespaceDeleted() {
	stats.Record(context.Background(), mNamespacesDeleted.M(int64(1)))
}
