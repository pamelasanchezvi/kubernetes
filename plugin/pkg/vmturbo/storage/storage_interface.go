package storage

import (
	"k8s.io/kubernetes/plugin/pkg/vmturbo/storage/vmtruntime"
	"k8s.io/kubernetes/plugin/pkg/vmturbo/storage/watch"
)

type Storage interface {
	Create(key string, obj, out interface{}, ttl uint64) error
	List(key string, listObj vmtruntime.VMTObject) error
	Get(key string, objPtr interface{}, ignoreNotFound bool) error
	Delete(key string, out interface{}) error
	Watch(key string, resourceVersion uint64, filter FilterFunc) (watch.Interface, error)
}

// FilterFunc is a predicate which takes an API object and returns true
// iff the object should remain in the set.
type FilterFunc func(obj interface{}) bool
