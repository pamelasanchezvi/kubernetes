package registry

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/storage"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/watch"

	"github.com/golang/glog"
)

// events implements Events interface
type vmtevents struct {
	client      *client.Client
	namespace   string
	etcdStorage storage.Interface
}

// newEvents returns a new events object.
func NewVMTEvents(c *client.Client, ns string, etcd storage.Interface) *vmtevents {
	return &vmtevents{
		client:      c,
		namespace:   ns,
		etcdStorage: etcd,
	}
}

// Create makes a new vmtevent. Returns the copy of the vmtevent the server returns,
// or an error.
func (e *vmtevents) Create(event *VMTEvent) (*VMTEvent, error) {
	if e.namespace != "" && event.Namespace != e.namespace {
		return nil, fmt.Errorf("can't create an event with namespace '%v' in namespace '%v'", event.Namespace, e.namespace)
	}
	api.Scheme.AddKnownTypes("", &VMTEvent{})
	out, err := e.create(event)
	if err != nil {
		return nil, err
	}
	result := out.(*VMTEvent)
	return result, err
}

// Create inserts a new item according to the unique key from the object.
func (e *vmtevents) create(obj runtime.Object) (runtime.Object, error) {
	key := "/vmtevents/"
	name := obj.(*VMTEvent).Name
	key = key + name
	ttl := uint64(10000)

	glog.Infof("About to create object")
	out := &VMTEvent{}
	if err := e.etcdStorage.Create(key, obj, out, ttl); err != nil {
		return nil, err
	}
	glog.Infof("Object created")
	return out, nil
}

// // Get returns the given event, or an error.
// func (e *vmtevents) Get(name string) (*VMTEvent, error) {
// 	result := &VMTEvent{}
// 	err := e.client.Get().
// 		NamespaceIfScoped(e.namespace, len(e.namespace) > 0).
// 		Resource("vmtevents").
// 		Name(name).
// 		Do().
// 		Into(result)
// 	return result, err
// }

// Get retrieves the item from etcd.
func (e *vmtevents) Get() (runtime.Object, error) {
	obj := &VMTEvent{}
	key := "/vmtevents/1"
	e.List()

	if err := e.etcdStorage.Get(key, obj, false); err != nil {
		return nil, err
	}
	return obj, nil
}

// List returns a list of events matching the selectors.
func (e *vmtevents) List() (*VMTEventList, error) {
	result := &VMTEventList{}
	r, err := e.ListPredicate()
	result = r.(*VMTEventList)
	return result, err
}

// ListPredicate returns a list of all the items matching m.
func (e *vmtevents) ListPredicate() (runtime.Object, error) {
	list := &VMTEventList{}
	rootKey := "/vmtevents/"
	err := e.etcdStorage.List(rootKey, list)
	if err != nil {
		return nil, err
	}
	glog.Infof("The list is %v", list)
	return list, err
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
func GenerateVMTEvent(actionType, namespace, targetSE, destination string, messageId int) *VMTEvent {

	event := makeVMTEvent(actionType, namespace, targetSE, destination, messageId)
	// event.Source = recorder.source

	return event
}

// Make a new VMTEvent instance.
func makeVMTEvent(actionType, namespace, targetSE, destination string, messageId int) *VMTEvent {
	t := util.Now()
	if namespace == "" {
		namespace = api.NamespaceDefault
	}
	return &VMTEvent{
		ObjectMeta: ObjectMeta{
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
