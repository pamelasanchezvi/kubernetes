package registry

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/watch"

	// "github.com/golang/glog"
)

// events implements Events interface
type vmtevents struct {
	client    *client.Client
	namespace string
}

// newEvents returns a new events object.
func NewVMTEvents(c *client.Client, ns string) *vmtevents {
	return &vmtevents{
		client:    c,
		namespace: ns,
	}
}

// Create makes a new vmtevent. Returns the copy of the vmtevent the server returns,
// or an error.
func (e *vmtevents) Create(event *api.VMTEvent) (*api.VMTEvent, error) {
	if e.namespace != "" && event.Namespace != e.namespace {
		return nil, fmt.Errorf("can't create an event with namespace '%v' in namespace '%v'", event.Namespace, e.namespace)
	}
	result := &api.VMTEvent{}
	err := e.client.Post().
		NamespaceIfScoped(event.Namespace, len(event.Namespace) > 0).
		Resource("vmtevents").
		Body(event).
		Do().
		Into(result)
	return result, err
}

// Get returns the given event, or an error.
func (e *vmtevents) Get(name string) (*api.VMTEvent, error) {
	result := &api.VMTEvent{}
	err := e.client.Get().
		NamespaceIfScoped(e.namespace, len(e.namespace) > 0).
		Resource("vmtevents").
		Name(name).
		Do().
		Into(result)
	return result, err
}

// List returns a list of events matching the selectors.
func (e *vmtevents) List() (*api.VMTEventList, error) {
	result := &api.VMTEventList{}
	err := e.client.Get().
		NamespaceIfScoped(e.namespace, len(e.namespace) > 0).
		Resource("vmtevents").
		Do().
		Into(result)
	return result, err
}

// Watch starts watching for vmtevents matching the given selectors.
func (e *vmtevents) Watch(label labels.Selector, field fields.Selector, resourceVersion string) (watch.Interface, error) {
	return e.client.Get().
		Prefix("watch").
		NamespaceIfScoped(e.namespace, len(e.namespace) > 0).
		Resource("vmtevents").
		Param("resourceVersion", resourceVersion).
		LabelsSelectorParam(label).
		FieldsSelectorParam(field).
		Watch()
}

// Delete deletes an existing event.
func (e *vmtevents) Delete(name string) error {
	return e.client.Delete().
		NamespaceIfScoped(e.namespace, len(e.namespace) > 0).
		Resource("vmtevents").
		Name(name).
		Do().
		Error()
}

func (e *vmtevents) DeleteAll() error {
	// events, err := e.List()
	// if err != nil {
	// 	return fmt.Errorf("Error listing all vmt events: %s", err)
	// }
	// for _, event := range events.Items {
	// 	errDeleteSingle := e.Delete(event.Name)
	// 	if errDeleteSingle != nil {
	// 		return fmt.Errorf("Error delete %s: %s", event.Name, errDeleteSingle)
	// 	}
	// }
	return e.client.Delete().
		NamespaceIfScoped(e.namespace, len(e.namespace) > 0).
		Resource("vmtevents").
		Do().
		Error()
}

// Build a new VMTEvent.
func GenerateVMTEvent(actionType, namespace, targetSE, destination string, messageId int) *api.VMTEvent {

	event := makeVMTEvent(actionType, namespace, targetSE, destination, messageId)
	// event.Source = recorder.source

	return event
}

// Make a new VMTEvent instance.
func makeVMTEvent(actionType, namespace, targetSE, destination string, messageId int) *api.VMTEvent {
	t := util.Now()
	if namespace == "" {
		namespace = api.NamespaceDefault
	}
	return &api.VMTEvent{
		ObjectMeta: api.ObjectMeta{
			Name:      fmt.Sprintf("%v.%x", targetSE, t.UnixNano()),
			Namespace: namespace,
		},
		ActionType:     actionType,
		TargetSE:       targetSE,
		Destination:    destination,
		VMTMessageID:   messageId,
		FirstTimestamp: t,
		LastTimestamp:  t,
		Count:          messageId,
	}
}
