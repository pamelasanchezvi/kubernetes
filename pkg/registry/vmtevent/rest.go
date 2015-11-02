/*
Copyright 2014 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vmtevent

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/api/validation"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/registry/generic"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util/fielderrors"
)

type eventStrategy struct {
	runtime.ObjectTyper
	api.NameGenerator
}

// Strategy is the default logic that pplies when creating and updating
// Event objects via the REST API.
var Strategy = eventStrategy{api.Scheme, api.SimpleNameGenerator}

func (eventStrategy) NamespaceScoped() bool {
	return true
}

func (eventStrategy) PrepareForCreate(obj runtime.Object) {
}

func (eventStrategy) PrepareForUpdate(obj, old runtime.Object) {
}

func (eventStrategy) Validate(ctx api.Context, obj runtime.Object) fielderrors.ValidationErrorList {
	event := obj.(*api.VMTEvent)
	return validation.ValidateVMTEvent(event)
}

func (eventStrategy) AllowCreateOnUpdate() bool {
	return true
}

func (eventStrategy) ValidateUpdate(ctx api.Context, obj, old runtime.Object) fielderrors.ValidationErrorList {
	event := obj.(*api.VMTEvent)
	return validation.ValidateVMTEvent(event)
}

func (eventStrategy) AllowUnconditionalUpdate() bool {
	return true
}

func MatchVMTEvent(label labels.Selector, field fields.Selector) generic.Matcher {
	return &generic.SelectionPredicate{Label: label, Field: field, GetAttrs: getAttrs}
}

func getAttrs(obj runtime.Object) (objLabels labels.Set, objFields fields.Set, err error) {
	event, ok := obj.(*api.VMTEvent)
	if !ok {
		return nil, nil, errors.NewInternalError(fmt.Errorf("object is not of type event: %#v", obj))
	}
	l := event.Labels
	if l == nil {
		l = labels.Set{}
	}
	return l, fields.Set{
		"metadata.name": event.Name,
		"actionType":    event.ActionType,
		"destination":   event.Destination,
		"targetSE":      event.TargetSE,
	}, nil
}
