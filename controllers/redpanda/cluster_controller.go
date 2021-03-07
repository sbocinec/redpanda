// Copyright 2021 Vectorized, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package redpanda contains reconciliation logic for redpanda.vectorized.io CRD
package redpanda

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	redpandav1alpha1 "github.com/vectorizedio/redpanda/src/go/k8s/apis/redpanda/v1alpha1"
	"github.com/vectorizedio/redpanda/src/go/k8s/pkg/labels"
	"github.com/vectorizedio/redpanda/src/go/k8s/pkg/resources"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	errNonexistentLastObservesState = errors.New("expecting to have statefulset LastObservedState set but it's nil")
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=redpanda.vectorized.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=redpanda.vectorized.io,resources=clusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=redpanda.vectorized.io,resources=clusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;
//+kubebuilder:rbac:groups=cert-manager.io,resources=issuers;certificates,verbs=create;get;list;watch;
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the Cluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *ClusterReconciler) Reconcile(
	ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
	log := r.Log.WithValues("redpandacluster", req.NamespacedName)

	log.Info(fmt.Sprintf("Starting reconcile loop for %v", req.NamespacedName))
	defer log.Info(fmt.Sprintf("Finished reconcile loop for %v", req.NamespacedName))

	var redpandaCluster redpandav1alpha1.Cluster
	crb := resources.NewClusterRoleBinding(r.Client, &redpandaCluster, r.Scheme, log)
	if err := r.Get(ctx, req.NamespacedName, &redpandaCluster); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		if apierrors.IsNotFound(err) {
			if removeError := crb.RemoveSubject(ctx, req.NamespacedName); removeError != nil {
				return ctrl.Result{}, fmt.Errorf("unable to remove subject in ClusterroleBinding: %w", removeError)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("unable to retrieve Cluster resource: %w", err)
	}

	svc := resources.NewHeadlessService(r.Client, &redpandaCluster, r.Scheme, log)
	issuer := resources.NewIssuer(r.Client, &redpandaCluster, r.Scheme, log)
	cert := resources.NewCertificate(r.Client, &redpandaCluster, r.Scheme, issuer, svc.HeadlessServiceFQDN(), log)
	sts := resources.NewStatefulSet(r.Client, &redpandaCluster, r.Scheme, svc.HeadlessServiceFQDN(), svc.Key().Name, cert.SecretKey(), log)
	toApply := []resources.Resource{
		svc,
		resources.NewNodePortService(r.Client, &redpandaCluster, r.Scheme, log),
		resources.NewConfigMap(r.Client, &redpandaCluster, r.Scheme, svc.HeadlessServiceFQDN(), log),
		issuer,
		cert,
		resources.NewServiceAccount(r.Client, &redpandaCluster, r.Scheme, log),
		resources.NewClusterRole(r.Client, &redpandaCluster, r.Scheme, log),
		crb,
		sts,
	}

	for _, res := range toApply {
		err := res.Ensure(ctx)

		var e *resources.NeedToReconcileError
		if errors.As(err, &e) {
			log.Info(e.Error())
			return ctrl.Result{RequeueAfter: e.RequeueAfter}, nil
		}

		if err != nil {
			log.Error(err, "Failed to reconcile resource")
		}
	}

	var observedPods corev1.PodList

	err := r.List(ctx, &observedPods, &client.ListOptions{
		LabelSelector: labels.ForCluster(&redpandaCluster).AsClientSelector(),
		Namespace:     redpandaCluster.Namespace,
	})
	if err != nil {
		log.Error(err, "Unable to fetch PodList resource")

		return ctrl.Result{}, err
	}

	observedNodes := make([]string, 0, len(observedPods.Items))
	// nolint:gocritic // the copies are necessary for further redpandacluster updates
	for _, item := range observedPods.Items {
		observedNodes = append(observedNodes, fmt.Sprintf("%s.%s", item.Name, svc.HeadlessServiceFQDN()))
	}

	if !reflect.DeepEqual(observedNodes, redpandaCluster.Status.Nodes) {
		redpandaCluster.Status.Nodes = observedNodes
		if err := r.Status().Update(ctx, &redpandaCluster); err != nil {
			log.Error(err, "Failed to update RedpandaClusterStatus")

			return ctrl.Result{}, err
		}
	}

	if sts.LastObservedState == nil {
		return ctrl.Result{}, errNonexistentLastObservesState
	}

	if sts.LastObservedState != nil && !reflect.DeepEqual(sts.LastObservedState.Status.ReadyReplicas, redpandaCluster.Status.Replicas) {
		redpandaCluster.Status.Replicas = sts.LastObservedState.Status.ReadyReplicas
		if err := r.Status().Update(ctx, &redpandaCluster); err != nil {
			log.Error(err, "Failed to update RedpandaClusterStatus")

			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha1.Cluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
