package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

func RecordAddedInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, trigger string) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeNormal,
		string(LumigoEventReasonAddedInstrumentation),
		fmt.Sprintf("Adding Lumigo instrumentation (trigger: %s)", trigger),
	)
}

func RecordRemovedInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, trigger string) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeNormal,
		string(LumigoEventReasonRemovedInstrumentation),
		fmt.Sprintf("Removing Lumigo instrumentation (trigger: %s)", trigger),
	)
}

func RecordUpdatedInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, trigger string) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeNormal,
		string(LumigoEventReasonUpdatedInstrumentation),
		fmt.Sprintf("Updating Lumigo instrumentation (trigger: %s)", trigger),
	)
}

func RecordCannotAddInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, trigger string, err error) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeWarning,
		string(LumigoEventReasonCannotAddInstrumentation),
		fmt.Sprintf("Cannot add Lumigo instrumentation (trigger: %s): %s", trigger, err.Error()),
	)
}

func RecordCannotRemoveInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, trigger string, err error) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeWarning,
		string(LumigoEventReasonCannotRemoveInstrumentation),
		fmt.Sprintf("Cannot remove Lumigo instrumentation (trigger: %s): %s", trigger, err.Error()),
	)
}

func RecordCannotUpdateInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, trigger string, err error) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeWarning,
		string(LumigoEventReasonCannotUpdateInstrumentation),
		fmt.Sprintf("Cannot update Lumigo instrumentation (trigger: %s): %s", trigger, err.Error()),
	)
}
