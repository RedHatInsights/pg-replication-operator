package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-viper/mapstructure/v2"

	replicationv1alpha1 "github.com/RedHatInsights/pg-replication-operator/api/v1alpha1"
)

// LogicalReplicationReconciler reconciles a LogicalReplication object
type LogicalReplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=replication.console.redhat.com,resources=logicalreplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=replication.console.redhat.com,resources=logicalreplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=replication.console.redhat.com,resources=logicalreplications/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the LogicalReplication object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *LogicalReplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get the LogicalReplication object from the API
	var lr replicationv1alpha1.LogicalReplication
	if err := r.Client.Get(ctx, req.NamespacedName, &lr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	publishingDb, err := r.getCredentialsFromSecret(ctx, req, lr.Spec.Publication.SecretName)
	if err != nil {
		log.Error(err, "getting publication credentials")
		return ctrl.Result{Requeue: true}, nil
	}

	log.Info("publishing database", "databaseHost", publishingDb.Host, "databasePort", publishingDb.Port)

	return ctrl.Result{}, nil
}

// Get secret with database credentials by name
func (r *LogicalReplicationReconciler) getCredentialsFromSecret(ctx context.Context, req ctrl.Request, secretName string) (DatabaseCredentials, error) {
	var db DatabaseCredentials
	var secret corev1.Secret
	var err error

	nn := types.NamespacedName{
		Name:      secretName,
		Namespace: req.Namespace,
	}
	if err = r.Client.Get(ctx, nn, &secret); err != nil {
		return db, err
	}

	var data interface{}
	if len(secret.Data) > 0 {
		data = secret.Data
	} else if len(secret.StringData) > 0 {
		data = secret.StringData
	} else {
		return db, fmt.Errorf("no secret data")
	}

	err = mapstructure.WeakDecode(data, &db)
	return db, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *LogicalReplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&replicationv1alpha1.LogicalReplication{}).
		Complete(r)
}
